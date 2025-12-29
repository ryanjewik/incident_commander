# agents/moderator_agent/main.py
from __future__ import annotations

import json
import os
import time
from concurrent.futures import ThreadPoolExecutor, as_completed
from datetime import datetime, timezone
from typing import Any, Dict, List, Optional, Tuple

from common.kafka_client import build_kafka_config, ensure_topics, make_consumer, make_producer
from common.output_writer import write_agent_artifacts
from common.gemini_client import build_gemini_client, gemini_json_call


AGENT_NAME = "moderator_agent"

INCIDENTS_TOPIC = "incidents_created"
AGENT_RESULT_TOPIC = "agent_result"
AGENT_STARTED_TOPIC = "agent_started"
MODERATOR_DECISION_TOPIC = "moderator_decision"


def utc_now_iso() -> str:
    return datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")


def publish_json(producer, topic: str, payload: Dict[str, Any]) -> None:
    producer.produce(topic, json.dumps(payload).encode("utf-8"))
    producer.flush()


def _safe_get(d: Dict[str, Any], path: List[str], default=None):
    cur: Any = d
    for p in path:
        if not isinstance(cur, dict) or p not in cur:
            return default
        cur = cur[p]
    return cur


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

            if topic == AGENT_STARTED_TOPIC:
                seen_started.add(agent)
                # continue waiting for results
                continue

            if topic == AGENT_RESULT_TOPIC:
                # keep latest per agent
                got_results[agent] = payload

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
    gemini_key = os.environ["GEMINI_API_KEY"]

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

    gemini = build_gemini_client(gemini_key)

    print(f"[{AGENT_NAME}] consuming topic={INCIDENTS_TOPIC} expected_agents={expected_agents}")

    try:
        while True:
            msg = consumer.poll(1.0)
            if msg is None:
                continue
            if msg.error():
                print(f"[{AGENT_NAME}] consumer error: {msg.error()}")
                continue

            raw = json.loads(msg.value().decode("utf-8"))
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
            event = _safe_get(raw, ["raw_payload_ref", "event"], {}) or {}
            monitor = _safe_get(raw, ["raw_payload_ref", "monitor"], {}) or {}
            incident_header = {
                "incident_id": incident_id,
                "organization_id": org_id,
                "title": raw.get("title") or event.get("title"),
                "type": raw.get("type"),
                "timestamp": raw.get("timestamp"),
                "event_url": event.get("link"),
                "snapshot_url": event.get("snapshot_url"),
                "monitor": {
                    "id": raw.get("monitor_id") or monitor.get("alert_id"),
                    "query": monitor.get("query"),
                    "tags": event.get("tags"),
                    "alert_summary": monitor.get("alert_status_summary"),
                },
            }

            # Wait for agent results for this incident
            agent_msgs = collect_agent_results_for_incident(
                kafka_cfg=kafka_cfg,
                incident_id=incident_id,
                expected_agents=expected_agents,
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

            # Final synthesis
            final = build_final_decision_with_gemini(
                gemini_client=gemini,
                model=gemini_model,
                incident_header=incident_header,
                agent_cards=agent_cards,
            )

            final_payload = {
                "agent": AGENT_NAME,
                "incident_id": incident_id,
                "organization_id": org_id,
                "timestamp": utc_now_iso(),
                "status": "completed",
                "result": final,
                "inputs": {
                    "expected_agents": expected_agents,
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
