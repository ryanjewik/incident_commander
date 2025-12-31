# agents/moderator_agent/main.py
from __future__ import annotations

import json
import os
import time
from concurrent.futures import ThreadPoolExecutor, as_completed
from datetime import datetime, timezone, timedelta
from typing import Any, Dict, List, Optional, Tuple

from common.kafka_client import build_kafka_config, ensure_topics, make_consumer, make_producer
from common.output_writer import write_agent_artifacts
from common.gemini_client import build_gemini_client, gemini_json_call
import uuid
import pathlib


AGENT_NAME = "moderator_agent"

INCIDENTS_TOPIC = "incidents_created"
AGENT_RESULT_TOPIC = "agent_result"
AGENT_STARTED_TOPIC = "agent_started"
MODERATOR_DECISION_TOPIC = "moderator_decision"


def utc_now_iso() -> str:
    return datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")


def _load_env_file(path: str) -> None:
    """Simple .env loader: set variables into os.environ if not already set.
    Handles inline comments after a '#' by stripping them. Does not support complex quoting.
    """
    try:
        p = pathlib.Path(path)
        if not p.exists():
            return
        for raw in p.read_text(encoding="utf-8").splitlines():
            line = raw.strip()
            if not line or line.startswith("#"):
                continue
            if "=" not in line:
                continue
            k, v = line.split("=", 1)
            k = k.strip()
            v = v.strip()
            # Remove surrounding quotes
            if (v.startswith('"') and v.endswith('"')) or (v.startswith("'") and v.endswith("'")):
                v = v[1:-1]
            else:
                # Strip inline comment starting with #
                if "#" in v:
                    v = v.split("#", 1)[0].strip()
            if k and v and k not in os.environ:
                os.environ[k] = v
    except Exception as e:
        print(f"[{AGENT_NAME}] failed to load .env from {path}: {e}")


def publish_json(producer, topic: str, payload: Dict[str, Any]) -> None:
    try:
        base = payload.get("incident_id") or payload.get("query_id") or payload.get("organization_id") or ""
        key = f"{base}-{uuid.uuid4().hex}" if base else uuid.uuid4().hex
        producer.produce(topic, json.dumps(payload).encode("utf-8"), key=key.encode("utf-8"))
        producer.flush()
    except Exception as e:
        print(f"[{AGENT_NAME}] failed to publish to topic={topic}: {e}")


def _safe_get(d: Dict[str, Any], path: List[str], default=None):
    cur: Any = d
    for p in path:
        if not isinstance(cur, dict) or p not in cur:
            return default
        cur = cur[p]
    return cur


def _parse_iso_ts(s: Optional[str]) -> Optional[datetime]:
    """Parse a few common ISO-8601 forms produced by agents (with optional microseconds and trailing Z). Returns timezone-aware UTC datetime or None."""
    if not s or not isinstance(s, str):
        return None
    try:
        # Accept trailing Z (UTC) or offsets
        if s.endswith("Z"):
            base = s[:-1]
            try:
                return datetime.strptime(base, "%Y-%m-%dT%H:%M:%S.%f").replace(tzinfo=timezone.utc)
            except Exception:
                try:
                    return datetime.strptime(base, "%Y-%m-%dT%H:%M:%S").replace(tzinfo=timezone.utc)
                except Exception:
                    return None
        # Let fromisoformat handle offsets like +00:00
        return datetime.fromisoformat(s)
    except Exception:
        return None


def summarize_agent_result_for_moderator(agent_msg: Dict[str, Any]) -> Dict[str, Any]:
    """
    Create a compact summary card from a full agent_result message.
    This is CPU-only (no LLM), used to reduce prompt size.
    """
    agent = agent_msg.get("agent")
    status = agent_msg.get("status")
    result = agent_msg.get("result") or {}

    # Prefer the fields your logs agent already outputs
    summary = result.get("summary") or ""
    impact = result.get("impact_guess") or ""
    top_findings = result.get("top_findings") or []
    actions = result.get("recommended_actions_ranked") or []

    # Keep only top 3 actions to save context
    actions_small = []
    for a in actions[:3]:
        if isinstance(a, dict):
            actions_small.append(
                {
                    "action": a.get("action", ""),
                    "priority": a.get("priority", ""),
                    "why": a.get("why", ""),
                }
            )

    return {
        "agent": agent,
        "status": status,
        "summary": summary,
        "impact_guess": impact,
        "top_findings": top_findings[:5] if isinstance(top_findings, list) else [],
        "recommended_actions_top3": actions_small,
        "debug": agent_msg.get("debug", {}),
    }


def llm_compress_agent_result(
    *,
    gemini_client,
    model: str,
    agent_msg: Dict[str, Any],
) -> Dict[str, Any]:
    """
    Optional LLM-based compression (stronger than rule-based), run in threadpool.
    If it fails, we fall back to summarize_agent_result_for_moderator().
    """
    base = summarize_agent_result_for_moderator(agent_msg)

    prompt = {
        "task": "Compress this agent_result into a compact decision card for a moderator.",
        "rules": [
            "Return STRICT JSON only.",
            "Keep it short.",
            "Extract only the most decision-relevant info and include 'confidence' (0..1).",
        ],
        "output_schema": {
            "agent": "string",
            "confidence": "number",
            "key_signals": ["string"],
            "risks": ["string"],
            "top_actions": [{"action": "string", "priority": "string"}],
            "notes": "string",
        },
        "input": {
            "agent": agent_msg.get("agent"),
            "result": agent_msg.get("result", {}),
            "debug": agent_msg.get("debug", {}),
        },
    }

    try:
        card = gemini_json_call(
            client=gemini_client,
            model=model,
            system_instructions=(
                "You are a senior incident commander assistant. Return STRICT JSON only. "
                "Be concise and concrete."
            ),
            payload=prompt,
        )
        # Ensure agent name carried through
        if isinstance(card, dict) and "agent" not in card:
            card["agent"] = agent_msg.get("agent")
        return {"kind": "llm_card", "card": card, "fallback": base}
    except Exception as e:
        return {"kind": "fallback_only", "error": str(e), "card": base}


def build_final_decision_with_gemini(
    *,
    gemini_client,
    model: str,
    incident_header: Dict[str, Any],
    agent_cards: List[Dict[str, Any]],
) -> Dict[str, Any]:
    prompt = {
        "task": "Synthesize all agent results into one final incident recommendation.",
        "rules": [
            "Return STRICT JSON only (no markdown).",
            "Include explicit reasoning that references which agent evidence drove the decision.",
            "If agents disagree, explain how you resolved the conflict.",
            "Be actionable: give a short 'do_this_now' checklist.",
        ],
        "output_schema": {
            "incident_summary": "string",
            "severity_guess": "string",
            "most_likely_root_cause": "string",
            "confidence": "number",
            "do_this_now": ["string"],
            "next_60_minutes": ["string"],
            "what_to_monitor": ["string"],
            "tradeoffs_and_risks": ["string"],
            "agent_disagreements": [{"agent": "string", "disagreement": "string"}],
            "reasoning": "string",
        },
        "incident": incident_header,
        "agents": agent_cards,
    }

    return gemini_json_call(
        client=gemini_client,
        model=model,
        system_instructions=(
            "You are the incident moderator/synthesizer. "
            "Return STRICT JSON only. Provide clear, operational reasoning."
        ),
        payload=prompt,
    )


def build_nl_answer_with_gemini(*, gemini_client, model: str, incident_header: Dict[str, Any], agent_cards: List[Dict[str, Any]]) -> Dict[str, Any]:
    """
    For `new_nl_query` incidents: produce a concise direct answer to the user's question.
    This function is intent-aware: it inspects `incident_header["intent"]` and adapts
    the prompt and expected output. Always returns STRICT JSON with at least
    `incident_summary` and `confidence`.
    """
    intent = (incident_header or {}).get("intent", "unknown")

    base_rules = [
        "Return STRICT JSON only.",
        "Provide a one-line `incident_summary` and a numeric `confidence` (0..1).",
    ]

    # Default output schema includes conditional outcomes to cover simulation-style queries
    output_schema = {
        "incident_summary": "string",
        "confidence": "number",
        "reasoning": "string",
        "conditional_outcomes": [{"outcome": "string", "likelihood": "string", "checks": ["string"]}],
    }

    # Customize prompt by intent
    if intent == "simulation":
        task = "User asked a hypothetical simulation/what-if. Return plausible conditional outcomes, their likelihoods, and short concrete checks or queries to validate each outcome. Include a concise summary sentence at the top."
        base_rules.append("When presenting conditional outcomes, list 2-4 distinct scenarios with concise validation checks.")
    elif intent == "status_query":
        task = "User asked about current status. Prefer statements derived from metrics evidence; if metrics are absent, say evidence is insufficient and suggest immediate checks."
        base_rules.append("If precise metric values are available in agent evidence, include them briefly in `reasoning`.")
        # status queries rarely need long conditional lists
        output_schema["conditional_outcomes"] = []
    elif intent == "postmortem":
        task = "User asked for postmortem/after-the-fact analysis. Return a short summary, most-likely root cause(s), and 2-3 recommended follow-ups for deeper investigation."
        base_rules.append("Include an ordered 'recommended_checks' list when appropriate.")
        output_schema.update({"recommended_checks": ["string"]})
    elif intent == "deep_followup":
        task = "User asked to verify or validate. Provide concise verification steps, required evidence, and a short conclusion stating if current evidence supports or refutes the hypothesis."
        base_rules.append("Prioritize concrete checks and short commands or queries the user can run.")
        output_schema.update({"verification_steps": ["string"]})
    else:
        task = "Answer the user's natural language query concisely using available agent evidence. If evidence is insufficient, present concise next steps to obtain it."

    prompt = {
        "task": task,
        "rules": base_rules + [
            "If evidence is insufficient, be explicit about what is missing and how to get it.",
            "Keep the `incident_summary` to one sentence; place any extra detail under `reasoning` or `conditional_outcomes`.",
        ],
        "output_schema": output_schema,
        "incident": incident_header,
        "agents": agent_cards,
    }

    system_instructions = (
        "You are a helpful incident responder. Return STRICT JSON only. Always include `incident_summary` and `confidence`. "
        "Adapt the structure to the incident intent: for 'simulation' prefer conditional outcomes with checks; for 'status_query' prefer metric-derived conclusions; for 'postmortem' include recommended_checks; for 'deep_followup' include verification_steps."
    )

    return gemini_json_call(
        client=gemini_client,
        model=model,
        system_instructions=system_instructions,
        payload=prompt,
    )


def _normalize_severity(raw: Optional[str]) -> Optional[str]:
    if not raw:
        return None
    s = str(raw).strip().lower()
    # accept some common forms
    if s in ("critical", "crit", "sev0", "sev1"):
        return "critical"
    if s in ("high", "sev2", "sev3"):
        return "high"
    if s in ("medium", "med", "moderate", "sev4"):
        return "medium"
    if s in ("low", "minor", "sev5"):
        return "low"
    # map numeric confidence to buckets if provided as string numeric
    try:
        v = float(s)
        if v >= 0.9:
            return "critical"
        if v >= 0.7:
            return "high"
        if v >= 0.4:
            return "medium"
        return "low"
    except Exception:
        pass
    # fallback: if includes words
    if "critical" in s:
        return "critical"
    if "high" in s:
        return "high"
    if "medium" in s or "moderate" in s:
        return "medium"
    if "low" in s:
        return "low"
    return None


def _compute_severity_from_cards(agent_cards: List[Dict[str, Any]], final: Dict[str, Any]) -> str:
    # Try to use any explicit severity from final
    sev = _normalize_severity(final.get("severity_guess"))
    if sev:
        return sev

    # Try to inspect agent cards for confidence signals
    max_conf = 0.0
    for c in agent_cards:
        card = c.get("card") if isinstance(c, dict) else None
        if isinstance(card, dict):
            conf = card.get("confidence") or card.get("debug", {}).get("confidence")
            try:
                f = float(conf)
                if f > max_conf:
                    max_conf = f
            except Exception:
                pass

    # Heuristic buckets
    if max_conf >= 0.9:
        return "critical"
    if max_conf >= 0.7:
        return "high"
    if max_conf >= 0.4:
        return "medium"

    # If any agent reported 'error' or 'failed' signals in keys/risks, bump severity
    for c in agent_cards:
        card = c.get("card") if isinstance(c, dict) else None
        if not isinstance(card, dict):
            continue
        for key in ("key_signals", "risks", "top_findings", "notes"):
            v = card.get(key)
            if not v:
                continue
            text = json.dumps(v).lower()
            if "critical" in text or "data loss" in text or "service unavailable" in text:
                return "critical"
            if "error" in text or "failed" in text or "outage" in text:
                return "high"

    return "low"


def collect_agent_results_for_incident(
    *,
    kafka_cfg: Dict[str, str],
    incident_id: str,
    expected_agents: List[str],
    wait_seconds: int,
) -> List[Dict[str, Any]]:
    """
    Spins up a dedicated consumer at `latest` so we only gather new agent_result messages.
    """
    # Create a per-incident consumer (unique group) and subscribe to both
    # `agent_started` and `agent_result` so we can use started messages as
    # readiness signals and avoid races where agents publish before the
    # moderator's ephemeral consumer is created.
    group = f"moderator-agent-{incident_id}-{int(time.time())}"
    # Use `earliest` so we don't miss near-simultaneous publishes that may
    # have occurred just before the consumer started. We filter by
    # `incident_id` below.
    consumer = make_consumer(kafka_cfg, group_id=group, auto_offset_reset="earliest")
    consumer.subscribe([AGENT_RESULT_TOPIC, AGENT_STARTED_TOPIC])

    deadline = time.time() + wait_seconds
    got_results: Dict[str, Dict[str, Any]] = {}
    seen_started: set[str] = set()

    try:
        while time.time() < deadline:
            # Break early if all expected agents both started and reported
            if len(got_results) >= len(expected_agents) and len(seen_started) >= len(expected_agents):
                break

            msg = consumer.poll(1.0)
            if msg is None:
                continue
            if msg.error():
                print(f"[{AGENT_NAME}] agent consumer error: {msg.error()}")
                continue

            try:
                payload = json.loads(msg.value().decode("utf-8"))
            except Exception:
                continue

            if payload.get("incident_id") != incident_id:
                continue

            topic = msg.topic()
            agent = payload.get("agent")
            if not agent:
                continue

            # Only consider agents that are in the expected_agents list for
            # this incident. This prevents accepting stale or unrelated
            # agent results that happen to share an incident_id.
            if agent not in expected_agents:
                # skip started/results from unexpected agents
                continue

            # Optional recency check: ignore messages older than a configured
            # threshold or far in the future (clock skew). This helps avoid
            # accepting historical results that were emitted long ago.
            max_age = int(os.getenv("MAX_AGENT_MESSAGE_AGE_SECONDS", "120"))
            max_future = int(os.getenv("MAX_AGENT_FUTURE_SKEW_SECONDS", "60"))
            ts_raw = payload.get("timestamp")
            if ts_raw:
                parsed_ts = _parse_iso_ts(str(ts_raw))
                if parsed_ts:
                    now = datetime.now(timezone.utc)
                    age = (now - parsed_ts).total_seconds()
                    if age > max_age:
                        print(f"[{AGENT_NAME}] skipping message from agent={agent} due to old timestamp={ts_raw} age={int(age)}s (max={max_age}s)")
                        continue
                    if parsed_ts - now > timedelta(seconds=max_future):
                        print(f"[{AGENT_NAME}] skipping message from agent={agent} due to future timestamp={ts_raw}")
                        continue

            if topic == AGENT_STARTED_TOPIC:
                seen_started.add(agent)
                # continue waiting for results
                continue

            if topic == AGENT_RESULT_TOPIC:
                # keep latest per agent
                got_results[agent] = payload

        # Replay scan removed: we rely on live polling only to gather agent results.
        # This avoids delays caused by replay logic that can make the moderator
        # appear to 'start' later than other agents. If needed, a future
        # implementation may reintroduce a configurable, non-blocking replay.

        # Return results in stable expected order (if present)
        out: List[Dict[str, Any]] = []
        for a in expected_agents:
            if a in got_results:
                out.append(got_results[a])

        # Also include any unexpected agents that reported for this incident
        for a, p in got_results.items():
            if a not in expected_agents:
                out.append(p)

        return out
    finally:
        consumer.close()


def main() -> None:
    gemini_key = os.environ.get("GEMINI_API_KEY")

    # Use a stronger / larger-context model here
    # (You can override at deploy time)
    gemini_model = os.getenv("GEMINI_MODEL", "gemini-2.0-pro")

    wait_seconds = int(os.getenv("WAIT_FOR_AGENTS_SECONDS", "120"))
    expected_agents = [
        s.strip() for s in os.getenv("EXPECTED_AGENTS", "logs_traces_agent").split(",") if s.strip()
    ]

    max_workers = int(os.getenv("THREADPOOL_WORKERS", "6"))

    # Shared client.properties in common/
    client_props_path = os.getenv("CLIENT_PROPERTIES_PATH", "common/client.properties")

    kafka_cfg = build_kafka_config(client_props_path, os.environ)
    producer = make_producer(kafka_cfg)

    # Main consumer for incidents_created
    consumer = make_consumer(kafka_cfg, group_id="moderator-agent-v1", auto_offset_reset="earliest")
    consumer.subscribe([INCIDENTS_TOPIC])

    # Ensure the topics exist (including moderator_decision)
    ensure_topics(
        kafka_cfg,
        [INCIDENTS_TOPIC, AGENT_STARTED_TOPIC, AGENT_RESULT_TOPIC, MODERATOR_DECISION_TOPIC],
    )

    gemini = build_gemini_client(gemini_key) if gemini_key else None

    try:
        while True:
            msg = consumer.poll(1.0)
            if msg is None:
                continue
            if msg.error():
                print(f"[{AGENT_NAME}] consumer error: {msg.error()}")
                continue

            raw_text = msg.value().decode("utf-8") if msg.value() else ""
            try:
                raw = json.loads(raw_text)
            except Exception as e:
                print(f"[{AGENT_NAME}] failed to parse incoming message JSON: {e}; raw={raw_text!r}")
                try:
                    with open('/tmp/moderator_bad_payloads.log', 'a', encoding='utf-8') as bf:
                        bf.write(raw_text + '\n')
                except Exception:
                    pass
                continue
            incident_id = raw.get("incident_id", "")
            org_id = raw.get("organization_id", "")

            if not incident_id:
                continue

            # Publish started
            publish_json(
                producer,
                AGENT_STARTED_TOPIC,
                {
                    "agent": AGENT_NAME,
                    "incident_id": incident_id,
                    "organization_id": org_id,
                    "timestamp": utc_now_iso(),
                    "status": "started",
                },
            )

            # Build incident header context (small)
            # Prefer raw_payload.trigger_message for chat-initiated replies, else fall back to raw_payload_ref.event
            event = _safe_get(raw, ["raw_payload_ref", "event"], {}) or {}
            monitor = _safe_get(raw, ["raw_payload_ref", "monitor"], {}) or {}
            if raw.get("raw_payload"):
                trigger = raw.get("raw_payload", {}).get("trigger_message") or {}
                # merge trigger fields if present
                if trigger:
                    event = {**event, **trigger}
            # Ensure user's raw question text is explicit for NL replies
            user_question = (event.get("text") or event.get("Text") or event.get("message") or "")
            if user_question:
                event["user_question"] = user_question
            incident_header = {
                "incident_id": incident_id,
                "organization_id": org_id,
                "title": raw.get("title") or event.get("title"),
                "type": raw.get("type"),
                "timestamp": raw.get("timestamp"),
                "chat_history": raw.get("raw_payload", {}).get("chat_history", []),
                "user_question": event.get("user_question") or raw.get("raw_payload", {}).get("trigger_message", {}).get("text"),
                "event_url": event.get("link"),
                "snapshot_url": event.get("snapshot_url"),
                "monitor": {
                    "id": raw.get("monitor_id") or monitor.get("alert_id"),
                    "query": monitor.get("query"),
                    "tags": event.get("tags"),
                    "alert_summary": monitor.get("alert_status_summary"),
                },
            }

            # Determine expected agents for this incident. Prefer per-message list if provided,
            # otherwise fall back to the environment-provided `expected_agents`.
            # NOTE: The moderator must be started for EVERY incident_created event regardless
            # of the per-message `agents` list. We only use `per_incident_agents` to decide
            # which other agents' results the moderator should wait for.
            # Build the per-incident agents list. Default to the environment
            # provided `expected_agents` but prefer an explicit per-message
            # `agents` list when present. Always strip out the moderator's
            # own agent name if it was mistakenly included so the moderator
            # does not appear as one of the agents it should wait for.
            per_incident_agents = expected_agents
            if raw.get("agents"):
                try:
                    arr = raw.get("agents")
                    if isinstance(arr, list):
                        extracted = [str(x) for x in arr if x]
                        if len(extracted) > 0:
                            per_incident_agents = extracted
                except Exception:
                    pass

            # Defensive: ensure the moderator is never present in the
            # per-message agents list (it is always started independently).
            try:
                if isinstance(per_incident_agents, list):
                    per_incident_agents = [a for a in per_incident_agents if a != AGENT_NAME]
            except Exception:
                pass

            # Always start the moderator for any incident. Log when the moderator
            # was not explicitly included in the per-incident agents list to make
            # debugging easier.
            try:
                if isinstance(per_incident_agents, list) and AGENT_NAME not in per_incident_agents:
                    print(f"[{AGENT_NAME}] not listed in incident agents but starting anyway; using agents={per_incident_agents} for wait list")
            except Exception:
                pass

            # Wait for agent results for this incident.
            # The moderator should be activated for the incident but should not
            # include itself in the list of agents it waits for (otherwise it
            # would wait for its own result). Build `wait_agents` as the
            # per-incident list minus the moderator itself.
            wait_agents = [a for a in per_incident_agents if a != AGENT_NAME]
            agent_msgs = collect_agent_results_for_incident(
                kafka_cfg=kafka_cfg,
                incident_id=incident_id,
                expected_agents=wait_agents,
                wait_seconds=wait_seconds,
            )

            # Threadpool: create LLM compression cards concurrently
            agent_cards: List[Dict[str, Any]] = []
            with ThreadPoolExecutor(max_workers=max_workers) as pool:
                futures = [
                    pool.submit(
                        llm_compress_agent_result,
                        gemini_client=gemini,
                        model=gemini_model,
                        agent_msg=m,
                    )
                    for m in agent_msgs
                ]
                for f in as_completed(futures):
                    agent_cards.append(f.result())

            # Final synthesis:
            # - For `reply_nl_query`: produce a concise NL answer only (no auto-suggestions unless explicitly requested).
            # - For `new_nl_query`: produce both a concise answer and full decision but prefer concise summary.
            if incident_header.get("type") == "reply_nl_query":
                try:
                    nl_short = build_nl_answer_with_gemini(
                        gemini_client=gemini,
                        model=gemini_model,
                        incident_header=incident_header,
                        agent_cards=agent_cards,
                    )
                except Exception as e:
                    print(f"[{AGENT_NAME}] reply NL short answer synthesis failed: {e}")
                    nl_short = {"incident_summary": "Failed to generate an answer.", "confidence": 0}

                # For reply path, return the concise answer as the final result
                final = nl_short if isinstance(nl_short, dict) else {"incident_summary": str(nl_short)}
            elif incident_header.get("type") == "new_nl_query":
                # Produce a short NL answer and also synthesize the fuller decision
                try:
                    nl_short = build_nl_answer_with_gemini(
                        gemini_client=gemini,
                        model=gemini_model,
                        incident_header=incident_header,
                        agent_cards=agent_cards,
                    )
                except Exception as e:
                    print(f"[{AGENT_NAME}] NL short answer synthesis failed: {e}")
                    nl_short = {"incident_summary": "Failed to generate an answer.", "confidence": 0}

                try:
                    full_decision = build_final_decision_with_gemini(
                        gemini_client=gemini,
                        model=gemini_model,
                        incident_header=incident_header,
                        agent_cards=agent_cards,
                    )
                except Exception as e:
                    print(f"[{AGENT_NAME}] fallback full decision synthesis failed: {e}")
                    full_decision = {"incident_summary": nl_short.get("incident_summary", ""), "confidence": nl_short.get("confidence", 0)}

                # Merge: prefer concise incident_summary from nl_short but keep the
                # structured fields (severity_guess, most_likely_root_cause, do_this_now, etc.)
                if isinstance(full_decision, dict):
                    merged: Dict[str, Any] = full_decision.copy()
                else:
                    merged = {"incident_summary": str(full_decision)}

                if isinstance(nl_short, dict) and nl_short.get("incident_summary"):
                    merged["incident_summary"] = nl_short.get("incident_summary")
                # carry over concise confidence if present
                if isinstance(nl_short, dict) and nl_short.get("confidence") is not None:
                    merged["confidence"] = nl_short.get("confidence")

                final = merged
            else:
                # Full incident decision path for other types
                final = build_final_decision_with_gemini(
                    gemini_client=gemini,
                    model=gemini_model,
                    incident_header=incident_header,
                    agent_cards=agent_cards,
                )

            # Ensure final contains a normalized severity estimate (low/medium/high/critical)
            try:
                # If LLM provided an explicit severity, normalize it
                sev = _normalize_severity(final.get("severity_guess")) if isinstance(final, dict) else None
                if not sev:
                    sev = _compute_severity_from_cards(agent_cards, final if isinstance(final, dict) else {})
                if isinstance(final, dict):
                    final["severity_guess"] = sev
                else:
                    final = {"incident_summary": str(final), "severity_guess": sev}
            except Exception as e:
                print(f"[{AGENT_NAME}] severity normalization failed: {e}")

            final_payload = {
                "agent": AGENT_NAME,
                "incident_id": incident_id,
                "organization_id": org_id,
                "timestamp": utc_now_iso(),
                "status": "completed",
                "result": final,
                "inputs": {
                    "expected_agents": per_incident_agents,
                    "waited_agents": wait_agents,
                    "received_agents": [m.get("agent") for m in agent_msgs],
                    "wait_seconds": wait_seconds,
                },
            }

            artifacts = write_agent_artifacts(
                incident_id=incident_id,
                agent_name=AGENT_NAME,
                payload=final_payload,
            )
            final_payload["artifacts"] = artifacts

            publish_json(producer, MODERATOR_DECISION_TOPIC, final_payload)
            print(f"[{AGENT_NAME}] published moderator_decision incident_id={incident_id}")

    finally:
        consumer.close()


if __name__ == "__main__":
    main()
