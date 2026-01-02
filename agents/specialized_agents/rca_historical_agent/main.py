from __future__ import annotations

import json
import ast
import os
import time
from datetime import datetime, timezone
from typing import Any, Dict, List, Optional

from common.kafka_client import build_kafka_config, ensure_topics, make_consumer, make_producer
from common.output_writer import write_agent_artifacts
from common.gemini_client import build_gemini_client, gemini_json_call
from common.secrets_client import SecretsClient
import uuid


AGENT_NAME = "rca_historical_agent"

INCIDENTS_TOPIC = "incidents_created"
AGENT_STARTED_TOPIC = "agent_started"
AGENT_RESULT_TOPIC = "agent_result"


def utc_now_iso() -> str:
    return datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")


def publish_json(producer, topic: str, payload: Dict[str, Any]) -> None:
    # lightweight produce helper: produce and flush, print result to stdout
    text = json.dumps(payload)
    try:
        base = payload.get("incident_id") or payload.get("query_id") or payload.get("organization_id") or ""
        key = f"{base}-{uuid.uuid4().hex}" if base else uuid.uuid4().hex
        producer.produce(topic, text.encode("utf-8"), key=key.encode("utf-8"))
        # allow librdkafka to deliver callbacks and flush
        producer.poll(0)
        producer.flush(10)
        print(f"[producer] published topic={topic} key={key}")
    except Exception as e:
        print(f"[producer] publish exception for topic={topic}: {e}")


def _safe_get(d: Dict[str, Any], path: List[str], default=None):
    cur: Any = d
    for p in path:
        if not isinstance(cur, dict) or p not in cur:
            return default
        cur = cur[p]
    return cur


# -------------------------
# Firestore (incidents)
# -------------------------
def _get_firestore_client():
    """
    Preferred: FIREBASE_CREDENTIALS_PATH or GOOGLE_APPLICATION_CREDENTIALS points to a service account JSON.
    """
    import firebase_admin
    from firebase_admin import credentials
    from google.cloud import firestore

    if not firebase_admin._apps:
        creds_path = os.getenv("FIREBASE_CREDENTIALS_PATH") or os.getenv("GOOGLE_APPLICATION_CREDENTIALS")
        if not creds_path:
            raise RuntimeError("Missing FIREBASE_CREDENTIALS_PATH or GOOGLE_APPLICATION_CREDENTIALS for Firestore.")
        firebase_admin.initialize_app(credentials.Certificate(creds_path))

    return firestore.Client()


def fetch_incident_doc_by_id(db, *, incident_id: str, collection: str) -> Optional[Dict[str, Any]]:
    doc_ref = db.collection(collection).document(incident_id)
    snap = doc_ref.get()
    if not snap.exists:
        return None

    out = snap.to_dict() or {}
    out["_doc_id"] = snap.id
    return out



def fetch_related_incidents_by_alert_id(
    db,
    *,
    alert_id: str,
    collection: str,
    exclude_incident_id: str,
    limit: int,
) -> List[Dict[str, Any]]:
    """
    Pull most recent incidents with matching alert_id.
    Requires an index if you add order_by on created_at.
    """
    # Try the ordered query first (preferred). If Firestore requires a composite
    # index and returns a 400, fall back to an unordered query and sort in-Python.
    out: List[Dict[str, Any]] = []
    try:
        q = (
            db.collection(collection)
            .where("alert_id", "==", alert_id)
            .order_by("created_at", direction="DESCENDING")
            .limit(limit)
            .stream()
        )

        for doc in q:
            d = doc.to_dict() or {}
            if d.get("incident_id") == exclude_incident_id:
                continue
            d["_doc_id"] = doc.id
            out.append(d)
        return out
    except Exception as e:
        print(f"[{AGENT_NAME}] Firestore ordered query failed, falling back: {e}")

    # Fallback: query without ordering, then sort by created_at in-Python.
    q2 = db.collection(collection).where("alert_id", "==", alert_id).limit(limit).stream()
    for doc in q2:
        d = doc.to_dict() or {}
        if d.get("incident_id") == exclude_incident_id:
            continue
        d["_doc_id"] = doc.id
        out.append(d)

    # Attempt to sort by created_at (ISO timestamps sort lexicographically);
    # if absent, leave original order.
    try:
        out.sort(key=lambda x: x.get("created_at") or "", reverse=True)
    except Exception:
        pass

    # Trim to limit in case fallback returned more
    return out[:limit]


def build_rca_prompt(*, current_incident: Dict[str, Any], history: List[Dict[str, Any]]) -> Dict[str, Any]:
    """
    Keep historical payload small: moderator_result can be large, so truncate aggressively.
    """
    def _truncate(obj: Any, max_chars: int = 8000) -> Any:
        s = json.dumps(obj, ensure_ascii=False)
        if len(s) <= max_chars:
            return obj
        return {"_truncated": True, "preview": s[:max_chars]}

    current_card = {
        "incident_id": current_incident.get("incident_id"),
        "alert_id": current_incident.get("alert_id"),
        "title": current_incident.get("title"),
        "date": current_incident.get("date") or current_incident.get("created_at"),
        "moderator_result": _truncate(current_incident.get("moderator_result")),
        "metadata": _truncate(current_incident.get("metadata"), max_chars=4000),
    }

    hist_cards = []
    for h in history:
        hist_cards.append(
            {
                "incident_id": h.get("incident_id"),
                "date": h.get("date") or h.get("created_at"),
                "title": h.get("title"),
                "moderator_result": _truncate(h.get("moderator_result")),
            }
        )

    return {
        "task": "Use historical incidents with the same alert_id to produce an RCA-oriented comparison and playbook hints.",
        "rules": [
            "Return STRICT JSON only (no markdown).",
            "Focus on patterns across incidents: recurring root causes, recurring mitigations, and early signals.",
            "Call out what is different about the current incident vs history.",
            "Be concrete and operational (next actions, what to check).",
        ],
        "output_schema": {
            "historical_summary": "string",
            "recurring_root_causes": [{"cause": "string", "frequency_guess": "string", "evidence": ["string"]}],
            "recurring_mitigations": [{"mitigation": "string", "worked_when": "string", "caveats": "string"}],
            "current_vs_history": {
                "similarities": ["string"],
                "differences": ["string"],
                "most_likely_match": {"incident_id": "string", "why": "string"},
            },
            "do_this_now": ["string"],
            "preventative_actions": ["string"],
            "confidence": "number",
            "reasoning": "string",
        },
        "current_incident": current_card,
        "historical_incidents": hist_cards,
    }


def process_incident(raw: Dict[str, Any], producer, gemini, firestore_db, incidents_collection: str) -> None:
    incident_id = raw.get("incident_id", "")
    org_id = raw.get("organization_id", "")

    if not incident_id:
        return

    # Respect per-message agents list: if provided, only run when this agent is included
    per_incident_agents = raw.get("agents")
    if per_incident_agents and isinstance(per_incident_agents, list):
        try:
            names = [str(x) for x in per_incident_agents if x]
        except Exception:
            names = []
        if AGENT_NAME not in names:
            print(f"[{AGENT_NAME}] skipping incident_id={incident_id} because not in agents list {names}")
            return

    # announce publishing agent_started (kept as log statement)
    print(f"[{AGENT_NAME}] publishing agent_started for incident_id={incident_id} org={org_id}")
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

    # If this is an NL query incident (new or reply), compare with recent 'new_nl_query' incidents' moderator_result
    if raw.get("type") in ("new_nl_query", "reply_nl_query"):
        print(f"[{AGENT_NAME}] performing NL-query historical compare for incident={incident_id}")
        limit = int(os.getenv("RCA_HISTORY_LIMIT", "10"))

        # Attempt to fetch the current incident doc to read alert_id and other metadata
        try:
            current_doc = fetch_incident_doc_by_id(firestore_db, incident_id=incident_id, collection=incidents_collection)
        except Exception as e:
            print(f"[{AGENT_NAME}] failed to fetch current incident doc: {e}")
            current_doc = None

        # Helper to extract moderator_result from a Firestore doc dict
        def _extract_moderator_result(d: Dict[str, Any]):
            # Look for several possible places/names where moderator decisions
            # may have been stored. Normalize to a single dict or value.
            candidates = []
            if isinstance(d.get("event"), dict):
                ev = d.get("event")
                candidates.append(ev.get("moderator_result"))
                candidates.append(ev.get("moderator_decision"))
                candidates.append(ev.get("moderator_results"))
            candidates.append(d.get("moderator_result"))
            candidates.append(d.get("moderator_decision"))
            candidates.append(d.get("moderator_results"))

            # Also check nested raw_payload or payload fields commonly used
            if isinstance(d.get("raw_payload"), dict):
                rp = d.get("raw_payload")
                candidates.append(rp.get("moderator_result"))
                candidates.append(rp.get("moderator_decision"))

            for c in candidates:
                if c:
                    return c
            return None

        history: List[Dict[str, Any]] = []

        # If this is a reply_nl_query, prefer matching by alert_id
        if raw.get("type") == "reply_nl_query" and current_doc:
            alert_id = current_doc.get("alert_id")
            if alert_id:
                try:
                    history = fetch_related_incidents_by_alert_id(
                        firestore_db,
                        alert_id=str(alert_id),
                        collection=incidents_collection,
                        exclude_incident_id=incident_id,
                        limit=limit,
                    )
                    # convert fetched docs to history entries with moderator_result
                    history = [
                        {
                            "incident_id": h.get("incident_id") or h.get("_doc_id"),
                            "moderator_result": _extract_moderator_result(h),
                        }
                        for h in history
                    ]
                except Exception as e:
                    print(f"[{AGENT_NAME}] fetch_related_incidents_by_alert_id failed: {e}")

        # If no alert_id matches (or not reply type), fall back to recent new_nl_query incidents for the org
        if not history:
            try:
                q = (
                    firestore_db.collection(incidents_collection)
                    .where("organization_id", "==", org_id)
                    .where("type", "==", "new_nl_query")
                    .order_by("created_at", direction="DESCENDING")
                    .limit(limit + 1)
                    .stream()
                )
                for doc in q:
                    d = doc.to_dict() or {}
                    inc_id = d.get("id") or d.get("incident_id") or doc.id
                    if inc_id == incident_id:
                        continue
                    mod = _extract_moderator_result(d)
                    history.append({"incident_id": inc_id, "moderator_result": mod})
            except Exception as e:
                print(f"[{AGENT_NAME}] Firestore org-based ordered query failed, falling back: {e}")
                # fallback stream scan
                try:
                    stream_limit = (limit + 1) * 5
                    stream_iter = firestore_db.collection(incidents_collection).stream()
                    fallback_hist = []
                    for doc in stream_iter:
                        if len(fallback_hist) >= stream_limit:
                            break
                        d = doc.to_dict() or {}
                        if d.get("organization_id") != org_id:
                            continue
                        if d.get("type") != "new_nl_query":
                            continue
                        inc_id = d.get("id") or d.get("incident_id") or doc.id
                        if inc_id == incident_id:
                            continue
                        mod = _extract_moderator_result(d)
                        fallback_hist.append({"incident_id": inc_id, "moderator_result": mod, "created_at": d.get("created_at")})

                    fallback_hist.sort(key=lambda x: x.get("created_at") or "", reverse=True)
                    history = fallback_hist[:limit]
                except Exception as e2:
                    print(f"[{AGENT_NAME}] org-based fallback stream scan also failed: {e2}")

        # At this point history is populated (possibly empty). Evaluate and synthesize
        if not history:
            result = {"summary": "No previous matching NL incidents found.", "confidence": 0.2}
        else:
            has_mod = any(h.get("moderator_result") for h in history)
            if not has_mod:
                result = {"summary": "Previous matching incidents found but they lack moderator_result fields.", "confidence": 0.3, "history_count": len(history)}
            else:
                payload = {
                    "task": "Compare moderator_result cards from recent matching NL queries and surface common signals, differences, and recommended next steps.",
                    "current_incident_id": incident_id,
                    "history": [h for h in history if h.get("moderator_result")],
                    "chat_history": raw.get("raw_payload", {}).get("chat_history", []),
                }
                try:
                    res = gemini_json_call(client=gemini, model=os.getenv("GEMINI_MODEL", "gemini-2.0-flash"), system_instructions=("Return STRICT JSON only: {\"comparison\": \"string\", \"common_signals\": [\"string\"], \"differences\": [\"string\"], \"recommendations\": [\"string\"], \"confidence\": \"number\"}"), payload=payload)
                    if not isinstance(res, dict):
                        res = {"comparison": str(res), "confidence": 0}
                    result = res
                    if isinstance(result, dict):
                        result.setdefault("debug", {})
                        result["debug"]["history_count"] = len(history)
                except Exception as e:
                    print(f"[{AGENT_NAME}] Gemini compare failed: {e}")
                    result = {"summary": "Failed to synthesize historical moderator_results.", "confidence": 0.2}

        final_payload = {
            "agent": AGENT_NAME,
            "incident_id": incident_id,
            "organization_id": org_id,
            "timestamp": utc_now_iso(),
            "status": "completed",
            "result": result,
        }
        artifacts = write_agent_artifacts(incident_id=incident_id, agent_name=AGENT_NAME, payload=final_payload)
        final_payload["artifacts"] = artifacts
        publish_json(producer, AGENT_RESULT_TOPIC, final_payload)
        print(f"[{AGENT_NAME}] completed NL-query historical compare for incident_id={incident_id}")
        return

    current_doc = fetch_incident_doc_by_id(
    firestore_db,
    incident_id=incident_id,
    collection=incidents_collection,
)


    if not current_doc:
        result = {
            "summary": "No incident doc found in Firestore for incident_id; cannot do historical RCA.",
            "confidence": 0.2,
            "reasoning": f"Firestore query returned no matching doc for incident_id={incident_id}.",
        }
    else:
        alert_id = current_doc.get("alert_id")
        if not alert_id:
            result = {
                "summary": "Incident doc missing alert_id; cannot find related incidents.",
                "confidence": 0.3,
                "reasoning": "Historical matching requires alert_id.",
            }
        else:
            limit = int(os.getenv("RCA_HISTORY_LIMIT", "10"))
            history = fetch_related_incidents_by_alert_id(
                firestore_db,
                alert_id=str(alert_id),
                collection=incidents_collection,
                exclude_incident_id=incident_id,
                limit=limit,
            )

            prompt = build_rca_prompt(current_incident=current_doc, history=history)
            result = gemini_json_call(
                client=gemini,
                model=os.getenv("GEMINI_MODEL", "gemini-2.0-flash"),
                system_instructions=(
                    "You are an incident RCA assistant. Return STRICT JSON only. "
                    "If historical data is sparse, say so and reduce confidence."
                ),
                payload=prompt,
            )

            # Helpful debug fields for the moderator
            if isinstance(result, dict):
                result.setdefault("debug", {})
                result["debug"].update(
                    {
                        "alert_id": alert_id,
                        "history_count": len(history),
                        "history_incident_ids": [h.get("incident_id") for h in history if h.get("incident_id")],
                    }
                )

    final_payload = {
        "agent": AGENT_NAME,
        "incident_id": incident_id,
        "organization_id": org_id,
        "timestamp": utc_now_iso(),
        "status": "completed",
        "result": result,
    }

    artifacts = write_agent_artifacts(incident_id=incident_id, agent_name=AGENT_NAME, payload=final_payload)
    final_payload["artifacts"] = artifacts

    publish_json(producer, AGENT_RESULT_TOPIC, final_payload)
    print(f"[{AGENT_NAME}] completed incident_id={incident_id}")


def main() -> None:
    # Kafka
    client_props_path = os.getenv("CLIENT_PROPERTIES_PATH", "common/client.properties")
    kafka_cfg = build_kafka_config(client_props_path, os.environ)
    ensure_topics(kafka_cfg, [INCIDENTS_TOPIC, AGENT_STARTED_TOPIC, AGENT_RESULT_TOPIC])
    group_id = os.getenv("KAFKA_GROUP_ID", "rca-historical-agent-v1")
    consumer = make_consumer(kafka_cfg, group_id=group_id, auto_offset_reset="earliest")
    producer = make_producer(kafka_cfg)
    consumer.subscribe([INCIDENTS_TOPIC])

    # Gemini
    gemini_key = os.environ["GEMINI_API_KEY"]
    gemini = build_gemini_client(gemini_key)

    # SecretsClient (Datadog creds available if you later enrich with DD queries)
    # This matches your move away from .env secrets.
    _ = SecretsClient()  # instantiated for parity; use _.get_datadog_keys(org_id) when needed.

    # Firestore
    incidents_collection = os.getenv("FIRESTORE_INCIDENTS_COLLECTION", "incidents")
    firestore_db = _get_firestore_client()

    print(f"[{AGENT_NAME}] consuming topic={INCIDENTS_TOPIC} collection={incidents_collection} group_id={group_id}")

    while True:
        msg = consumer.poll(1.0)
        if msg is None:
            continue
        if msg.error():
            print(f"[{AGENT_NAME}] consumer error: {msg.error()}")
            continue

        try:
            raw_bytes = msg.value()
            raw_text = raw_bytes.decode("utf-8", errors="replace") if raw_bytes else ""
            try:
                raw = json.loads(raw_text)
            except json.JSONDecodeError:
                print(f"[{AGENT_NAME}] failed to decode JSON from kafka message, raw={raw_text[:1000]}")
                try:
                    val = ast.literal_eval(raw_text)
                    if isinstance(val, dict):
                        raw = val
                        print(f"[{AGENT_NAME}] parsed kafka message with ast.literal_eval fallback")
                    else:
                        print(f"[{AGENT_NAME}] ast.literal_eval produced non-dict type {type(val)}; skipping message")
                        continue
                except Exception as e:
                    print(f"[{AGENT_NAME}] ast.literal_eval fallback failed: {e}; skipping message")
                    continue
        except Exception as e:
            print(f"[{AGENT_NAME}] failed to read message bytes: {e}")
            continue

        try:
            process_incident(raw, producer, gemini, firestore_db, incidents_collection)
        except Exception as e:
            print(f"[{AGENT_NAME}] error: {e}")
            try:
                failure_payload = {
                    "agent": AGENT_NAME,
                    "incident_id": raw.get("incident_id") if isinstance(raw, dict) else None,
                    "organization_id": raw.get("organization_id") if isinstance(raw, dict) else None,
                    "timestamp": utc_now_iso(),
                    "status": "failed",
                    "result": {"summary": "agent processing error", "raw_error": str(e)},
                }
                publish_json(producer, AGENT_RESULT_TOPIC, failure_payload)
            except Exception as e2:
                print(f"[{AGENT_NAME}] failed to publish error payload: {e2}")


if __name__ == "__main__":
    main()
