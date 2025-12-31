from __future__ import annotations
import json
import os
import time
from datetime import datetime, timezone
from typing import Any, Dict
from concurrent.futures import ThreadPoolExecutor, Future
import threading
import uuid

from agents.common.kafka_client import build_kafka_config, make_consumer, make_producer, ensure_topics
from agents.common.datadog_client import query_timeseries, get_monitor_details, download_snapshot_image, list_metrics
from agents.common.gemini_client import build_gemini_client, gemini_json_call
from agents.common.output_writer import write_agent_artifacts
from agents.common.secrets_client import SecretsClient
import traceback


AGENT_NAME = "metrics_agent"


def utc_now_iso() -> str:
    return datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")


def publish_json(producer, topic: str, payload: Dict[str, Any]) -> None:
    try:
        base = payload.get("incident_id") or payload.get("query_id") or payload.get("organization_id") or ""
        key = f"{base}-{uuid.uuid4().hex}" if base else uuid.uuid4().hex
        producer.produce(topic, json.dumps(payload).encode("utf-8"), key=key.encode("utf-8"))
        producer.flush(10)
    except Exception as e:
        print(f"[{AGENT_NAME}] failed to publish to topic={topic}: {e}")


def build_metric_queries(raw: Dict[str, Any], monitor_query: str) -> list[str]:
    queries: list[str] = []
    if monitor_query:
        queries.append(monitor_query)
    # Prefer tags from raw_payload_ref.event but fall back to trigger_message (reply_nl_query)
    tags = raw.get("raw_payload_ref", {}).get("event", {}).get("tags", "") or ""
    if raw.get("type") == "reply_nl_query":
        trigger = raw.get("raw_payload", {}).get("trigger_message") or {}
        tags = tags or (trigger.get("tags") or "")
    service = None
    env = None
    host = None
    container = None
    for t in tags.split(","):
        t = t.strip()
        if t.startswith("service:"):
            service = t.split(":", 1)[1]
        if t.startswith("env:"):
            env = t.split(":", 1)[1]
        if t.startswith("host:"):
            host = t.split(":", 1)[1]
        if t.startswith("container:"):
            container = t.split(":", 1)[1]

    if service and env:
        queries.append(f"avg:system.cpu.user{{service:{service},env:{env}}}")
        queries.append(f"avg:system.mem.used{{service:{service},env:{env}}}")
        queries.append(f"avg:system.load.1{{service:{service},env:{env}}}")
    elif service:
        queries.append(f"avg:system.cpu.user{{service:{service}}}")
        queries.append(f"avg:system.mem.used{{service:{service}}}")
        queries.append(f"avg:system.load.1{{service:{service}}}")
    elif env:
        queries.append(f"avg:system.cpu.user{{env:{env}}}")
        queries.append(f"avg:system.mem.used{{env:{env}}}")
        queries.append(f"avg:system.load.1{{env:{env}}}")
    # host or container specific fallbacks
    if host:
        queries.extend([
            f"avg:system.cpu.user{{host:{host}}}",
            f"avg:system.mem.used{{host:{host}}}",
            f"avg:system.load.1{{host:{host}}}",
        ])
    if container:
        queries.extend([
            f"avg:container.cpu.user{{container_name:{container}}}",
            f"avg:container.memory.usage{{container_name:{container}}}",
        ])

    # If we still have no queries, try to heuristically derive a service from title/text
    if not queries:
        title = raw.get("raw_payload_ref", {}).get("event", {}).get("title", "") or ""
        text_only = raw.get("raw_payload_ref", {}).get("event", {}).get("text_only_message", "") or ""
        if raw.get("type") == "reply_nl_query":
            trigger = raw.get("raw_payload", {}).get("trigger_message") or {}
            title = title or (trigger.get("title") or "")
            text_only = text_only or (trigger.get("text") or trigger.get("text_only_message") or "")
        # simple heuristics: look for 'service:NAME' tokens or 'svc=NAME'
        for src in (tags, title, text_only):
            for token in src.replace(";", " ").replace(",", " ").split():
                token = token.strip()
                if token.startswith("service:"):
                    service = token.split(":", 1)[1]
                    break
                if token.startswith("svc="):
                    service = token.split("=", 1)[1]
                    break
            if service:
                break

    # As a last resort, add generic system metrics (no tags) to try and find signal
    if not queries:
        queries.extend([
            "avg:system.cpu.user",
            "avg:system.cpu.system",
            "avg:system.cpu.idle",
            "avg:system.mem.used",
            "avg:system.load.1",
            "avg:container.cpu.user",
            "avg:container.cpu.system",
        ])

    # Deduplicate while preserving order
    seen = set()
    deduped: list[str] = []
    for q in queries:
        if not q:
            continue
        if q in seen:
            continue
        seen.add(q)
        deduped.append(q)

    return deduped


def process_incident(
    raw: Dict[str, Any],
    producer,
    gemini,
    dd_site: str,
    gemini_model: str,
    started_topic: str,
    result_topic: str,
    gemini_keys: list[str],
    key_index_holder: Dict[str, int],
    key_lock: threading.Lock,
    dd_sem=None,
    gemini_sem=None,
    secrets_client: SecretsClient | None = None,
) -> None:
    try:
        def _response_has_series(resp: Any) -> bool:
            try:
                if not resp:
                    return False
                if isinstance(resp, dict):
                    # Datadog v1/query returns a 'series' array when successful
                    if "series" in resp and isinstance(resp.get("series"), list) and len(resp.get("series")) > 0:
                        return True
                    # Some endpoints return 'data' -> check for non-empty
                    if "data" in resp and isinstance(resp.get("data"), list) and len(resp.get("data")) > 0:
                        return True
                return False
            except Exception:
                return False

        incident_id = raw.get("incident_id", "")
        org_id = raw.get("organization_id", "")
        
        # DEBUG: Log incident details and timestamp
        last_updated_ms = int(raw.get("raw_payload_ref", {}).get("event", {}).get("last_updated_ms", "0") or "0")
        incident_time = datetime.fromtimestamp(last_updated_ms / 1000, tz=timezone.utc) if last_updated_ms else None
        current_time = datetime.now(timezone.utc)
        age_minutes = (current_time - incident_time).total_seconds() / 60 if incident_time else None
        
        print(f"[{AGENT_NAME}] DEBUG: Processing incident_id={incident_id}, org_id={org_id}")
        print(f"[{AGENT_NAME}] DEBUG: Incident timestamp={incident_time}, age={age_minutes:.1f} minutes" if age_minutes else f"[{AGENT_NAME}] DEBUG: Incident timestamp unavailable")
        
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

        with key_lock:
            current_key_index = key_index_holder["index"]
        print(f"[{AGENT_NAME}] DEBUG: Using Gemini key index={current_key_index} of {len(gemini_keys)} available keys")
        
        publish_json(
            producer,
            started_topic,
            {
                "agent": AGENT_NAME,
                "incident_id": incident_id,
                "organization_id": org_id,
                "timestamp": utc_now_iso(),
                "status": "started",
            },
        )

        # For NL queries (new or reply), derive a timeframe from the incident timestamp and continue normal metric analysis
        if raw.get("type") in ("new_nl_query", "reply_nl_query"):
            print(f"[{AGENT_NAME}] received {raw.get('type')} incident={incident_id}, deriving time window from timestamp")
            # Try to read an explicit timestamp field (RFC3339) published by backend
            ts_str = raw.get("timestamp") or raw.get("created_at") or raw.get("raw_payload_ref", {}).get("event", {}).get("timestamp")
            # For reply_nl_query, prefer trigger_message timestamp
            if raw.get("type") == "reply_nl_query":
                trigger = raw.get("raw_payload", {}).get("trigger_message") or {}
                ts_str = ts_str or trigger.get("created_at") or trigger.get("timestamp")

            if ts_str:
                try:
                    dt = datetime.fromisoformat(str(ts_str).replace("Z", "+00:00"))
                    last_updated_ms = int(dt.timestamp() * 1000)
                except Exception:
                    # fall back to existing last_updated_ms (may be 0)
                    pass
            else:
                # Fallback: use now
                last_updated_ms = int(datetime.now(timezone.utc).timestamp() * 1000)

            print(f"[{AGENT_NAME}] DEBUG: computed last_updated_ms={last_updated_ms} from incident timestamp")

        from_ms = max(0, last_updated_ms - 30 * 60 * 1000)
        to_ms = last_updated_ms + 5 * 60 * 1000
        print(f"[{AGENT_NAME}] DEBUG: Time window: from_ms={from_ms}, to_ms={to_ms}")

        monitor_id_str = raw.get("monitor_id", "")
        monitor_id = None
        if isinstance(monitor_id_str, str) and monitor_id_str.isdigit():
            monitor_id = int(monitor_id_str)
        print(f"[{AGENT_NAME}] DEBUG: monitor_id={monitor_id}")

        # Resolve per-organization Datadog keys (backend-decrypted).
        # Do NOT fall back to environment variables for Datadog API/app keys.
        dd_api_key_used = None
        dd_app_key_used = None
        try:
            if secrets_client and org_id:
                print(f"[{AGENT_NAME}] DEBUG: Fetching Datadog keys for org_id={org_id}")
                sk = secrets_client.get_datadog_keys(org_id)
                if sk:
                    dd_api_key_used = sk.get("datadog_api_key")
                    dd_app_key_used = sk.get("datadog_app_key")
                    print(f"[{AGENT_NAME}] DEBUG: Successfully retrieved Datadog keys for org_id={org_id}")
                else:
                    print(f"[{AGENT_NAME}] DEBUG: No Datadog keys returned for org_id={org_id}")
        except Exception as e:
            print(f"[{AGENT_NAME}] secrets fetch error for org={org_id}: {e}")

        # If we couldn't obtain decrypted keys for this organization, fail fast.
        if not dd_api_key_used or not dd_app_key_used:
            err_msg = f"missing datadog keys for org={org_id}; refusing to use .env values"
            print(f"[{AGENT_NAME}] {err_msg}")
            publish_json(
                producer,
                result_topic,
                {
                    "agent": AGENT_NAME,
                    "incident_id": incident_id,
                    "organization_id": org_id,
                    "timestamp": utc_now_iso(),
                    "status": "failed",
                    "error": err_msg,
                },
            )
            return

        monitor_details = {}
        monitor_query = ""
        if monitor_id:
            print(f"[{AGENT_NAME}] DEBUG: Fetching monitor details for monitor_id={monitor_id}")
            if dd_sem:
                with dd_sem:
                    monitor_details = get_monitor_details(
                        dd_site=dd_site,
                        dd_api_key=dd_api_key_used,
                        dd_app_key=dd_app_key_used,
                        monitor_id=monitor_id,
                    )
            else:
                monitor_details = get_monitor_details(
                    dd_site=dd_site,
                    dd_api_key=dd_api_key_used,
                    dd_app_key=dd_app_key_used,
                    monitor_id=monitor_id,
                )
            monitor_query = monitor_details.get("query", "")
            print(f"[{AGENT_NAME}] DEBUG: Monitor query='{monitor_query}'")

        metric_queries = build_metric_queries(raw, monitor_query)
        print(f"[{AGENT_NAME}] DEBUG: Built {len(metric_queries)} metric queries: {metric_queries}")

        timeseries_data = []
        attempted_queries = []

        def run_query(q: str, f_ms: int, t_ms: int):
            print(f"[{AGENT_NAME}] DEBUG: Querying timeseries: '{q}' from {f_ms} to {t_ms}")
            attempted_queries.append({"query": q, "from_ms": f_ms, "to_ms": t_ms})
            # Datadog query parser often requires an explicit scope expression.
            # If the query string lacks '{', add a wildcard scope '{*}' to allow global matching.
            query_for_api = q if "{" in q else f"{q}{{*}}"
            if dd_sem:
                with dd_sem:
                    return query_timeseries(
                        dd_site=dd_site,
                        dd_api_key=dd_api_key_used,
                        dd_app_key=dd_app_key_used,
                        query=query_for_api,
                        from_ms=f_ms,
                        to_ms=t_ms,
                    )
            else:
                return query_timeseries(
                    dd_site=dd_site,
                    dd_api_key=dd_api_key_used,
                    dd_app_key=dd_app_key_used,
                    query=query_for_api,
                    from_ms=f_ms,
                    to_ms=t_ms,
                )

        # Primary attempts: run the metric_queries for the computed window
        for idx, query in enumerate(metric_queries):
            if not query:
                continue
            ts_result = run_query(query, from_ms, to_ms)
            if _response_has_series(ts_result):
                timeseries_data.append({"query": query, "result": ts_result})
                print(f"[{AGENT_NAME}] DEBUG: Timeseries query returned data for '{query}'")
            else:
                print(f"[{AGENT_NAME}] DEBUG: Timeseries query returned no usable series for '{query}'")
                print(f"[{AGENT_NAME}] DEBUG: raw response: {ts_result}")

        # Fallbacks: if no data, try expanding time window and query variants
        if not timeseries_data:
            print(f"[{AGENT_NAME}] DEBUG: No data found for initial queries, attempting fallbacks")
            # try broader windows (minutes): 60, 180, 360, 1440 (24h), 10080 (7d)
            for expand_minutes in (60, 180, 360, 1440, 10080):
                f_ms = max(0, last_updated_ms - expand_minutes * 60 * 1000)
                t_ms = last_updated_ms + 5 * 60 * 1000
                print(f"[{AGENT_NAME}] DEBUG: Fallback window expand to {expand_minutes} minutes => from={f_ms}")
                for q in metric_queries:
                    ts_result = run_query(q, f_ms, t_ms)
                    if _response_has_series(ts_result):
                        timeseries_data.append({"query": q, "result": ts_result, "window_minutes": expand_minutes})
                        print(f"[{AGENT_NAME}] DEBUG: Fallback query returned data for '{q}' (expanded {expand_minutes}m)")
                    else:
                        print(f"[{AGENT_NAME}] DEBUG: Fallback query returned no usable series for '{q}' (expanded {expand_minutes}m)")
                        print(f"[{AGENT_NAME}] DEBUG: raw response: {ts_result}")
                if timeseries_data:
                    break

        # If still empty, try alternative aggregations (sum/min/max) for same tag sets
        if not timeseries_data:
            alt_queries = []
            for q in metric_queries:
                # Replace avg: with sum: and max: as alternatives
                if q.startswith("avg:"):
                    alt_queries.append(q.replace("avg:", "sum:", 1))
                    alt_queries.append(q.replace("avg:", "max:", 1))
                else:
                    alt_queries.append(q)
            for q in alt_queries:
                ts_result = run_query(q, from_ms, to_ms)
                if _response_has_series(ts_result):
                    timeseries_data.append({"query": q, "result": ts_result, "variant": True})
                    print(f"[{AGENT_NAME}] DEBUG: Alternative aggregation query returned data for '{q}'")
                else:
                    print(f"[{AGENT_NAME}] DEBUG: Alternative aggregation returned no usable series for '{q}'")
                    print(f"[{AGENT_NAME}] DEBUG: raw response: {ts_result}")
                    break

        # Last resort: if monitor_query exists, attempt a direct series extraction from monitor query variants
        if not timeseries_data and monitor_query:
            print(f"[{AGENT_NAME}] DEBUG: Attempting monitor-based fallback for monitor_query='{monitor_query}'")
            try:
                # attempt a small set of derived monitor queries
                derived = [monitor_query, monitor_query.replace("avg:", "sum:")]
                for q in derived:
                    ts_result = run_query(q, from_ms, to_ms)
                    if _response_has_series(ts_result):
                        timeseries_data.append({"query": q, "result": ts_result, "monitor_based": True})
                        print(f"[{AGENT_NAME}] DEBUG: Monitor-based query returned data for '{q}'")
                    else:
                        print(f"[{AGENT_NAME}] DEBUG: Monitor-based query returned no usable series for '{q}'")
                        print(f"[{AGENT_NAME}] DEBUG: raw response: {ts_result}")
                        break
            except Exception as e:
                print(f"[{AGENT_NAME}] DEBUG: monitor-based fallback failed: {e}")

        snapshot_url = raw.get("raw_payload_ref", {}).get("event", {}).get("snapshot_url", "") or ""
        snapshot_bytes = None
        if snapshot_url:
            print(f"[{AGENT_NAME}] DEBUG: Downloading snapshot from {snapshot_url}")
            try:
                snapshot_bytes = download_snapshot_image(snapshot_url)
                print(f"[{AGENT_NAME}] DEBUG: Snapshot downloaded, size={len(snapshot_bytes)} bytes")
            except Exception as e:
                print(f"[{AGENT_NAME}] snapshot download failed: {e}")

        event = raw.get("raw_payload_ref", {}).get("event", {}) or {}

        # If still no timeseries, attempt metric discovery using Datadog /api/v1/metrics
        if not timeseries_data:
            print(f"[{AGENT_NAME}] DEBUG: No timeseries from standard queries, attempting metric discovery")
            # derive simple search tokens from metric_queries (e.g., 'cpu', 'mem', 'load')
            tokens = set()
            import re
            for q in metric_queries:
                for m in re.findall(r"[a-zA-Z_]+", q):
                    if len(m) > 2:
                        tokens.add(m.lower())

            # derive tags (service/env/host/container) from raw to build scoped queries
            tags_src = raw.get("raw_payload_ref", {}).get("event", {}).get("tags", "") or ""
            if raw.get("type") == "reply_nl_query":
                trigger = raw.get("raw_payload", {}).get("trigger_message") or {}
                tags_src = tags_src or (trigger.get("tags") or "")
            svc_val = None
            env_val = None
            host_val = None
            for t in (p.strip() for p in tags_src.split(",") if p.strip()):
                if t.startswith("service:"):
                    svc_val = t.split(":", 1)[1]
                if t.startswith("env:"):
                    env_val = t.split(":", 1)[1]
                if t.startswith("host:"):
                    host_val = t.split(":", 1)[1]

            discovered_tried = []
            for token in tokens:
                # ask Datadog for metric names that match the token
                try:
                    candidates = list_metrics(
                        dd_site=dd_site,
                        dd_api_key=dd_api_key_used,
                        dd_app_key=dd_app_key_used,
                        query=token,
                        from_ms=from_ms,
                        to_ms=to_ms,
                        limit=20,
                    )
                except Exception as e:
                    print(f"[{AGENT_NAME}] metric discovery failed for token={token}: {e}")
                    candidates = []

                for metric_name in candidates:
                    if len(discovered_tried) > 50:
                        break
                    # build candidate queries with tag relaxations
                    cands = []
                    if svc_val and env_val:
                        cands.append(f"avg:{metric_name}{{service:{svc_val},env:{env_val}}}")
                    if svc_val:
                        cands.append(f"avg:{metric_name}{{service:{svc_val}}}")
                    if env_val:
                        cands.append(f"avg:{metric_name}{{env:{env_val}}}")
                    if host_val:
                        cands.append(f"avg:{metric_name}{{host:{host_val}}}")
                    cands.append(f"avg:{metric_name}")

                    for q in cands:
                        if q in discovered_tried:
                            continue
                        discovered_tried.append(q)
                        ts_result = run_query(q, from_ms, to_ms)
                        if _response_has_series(ts_result):
                            timeseries_data.append({"query": q, "result": ts_result, "discovered_metric": metric_name})
                            print(f"[{AGENT_NAME}] DEBUG: Discovery query returned data for '{q}' (metric={metric_name})")
                            break
                        else:
                            print(f"[{AGENT_NAME}] DEBUG: Discovery query returned no series for '{q}' (metric={metric_name})")
                    if timeseries_data:
                        break
                if timeseries_data:
                    break

        # Skip Gemini call if no timeseries data available
        if not timeseries_data:
            print(f"[{AGENT_NAME}] DEBUG: No timeseries data available, skipping Gemini analysis")
            final_payload = {
                "agent": AGENT_NAME,
                "incident_id": incident_id,
                "organization_id": org_id,
                "timestamp": utc_now_iso(),
                "status": "no_metrics_available",
                "result": {
                    "summary": "No metric data available for analysis. This incident may not be metrics-related or lacks proper service/env tags.",
                    "analysis": None,
                    "recommendations": None,
                },
                "debug": {
                    "monitor_query": monitor_query,
                    "metric_queries": metric_queries,
                    "from_ms": from_ms,
                    "to_ms": to_ms,
                    "snapshot_included": bool(snapshot_bytes),
                    "timeseries_count": 0,
                },
            }
            
            artifact_paths = write_agent_artifacts(
                incident_id=incident_id,
                agent_name=AGENT_NAME,
                payload=final_payload,
            )
            final_payload["artifacts"] = artifact_paths
            
            publish_json(producer, result_topic, final_payload)
            print(f"[{AGENT_NAME}] completed incident_id={incident_id} (no metrics)")
            return

        incident_context: Dict[str, Any] = {
            "incident_id": incident_id,
            "organization_id": org_id,
            "datadog_site": dd_site,
            "time_window_ms": {"from": from_ms, "to": to_ms},
            "monitor": {
                "id": monitor_id,
                "query": monitor_query,
                "title": monitor_details.get("name") or event.get("title"),
                "tags": event.get("tags"),
                "type": monitor_details.get("type"),
                "thresholds": monitor_details.get("thresholds"),
                "event_url": event.get("link"),
            },
            "snapshot_url": snapshot_url,
            "timeseries": timeseries_data,
            "chat_history": raw.get("raw_payload", {}).get("chat_history", []),
        }

        print(f"[{AGENT_NAME}] DEBUG: Calling Gemini for metrics analysis (JSON schema) with key_index={current_key_index}")
        # Build a structured payload and request strict JSON from Gemini to avoid markdown
        payload = {
            "task": "Analyze metrics and provide a concise JSON report.",
            "incident_context": {
                "incident_id": incident_id,
                "monitor": incident_context.get("monitor"),
                "time_window_ms": incident_context.get("time_window_ms"),
                "timeseries": incident_context.get("timeseries"),
            },
        }
        system_instructions = (
            "You are an incident metrics analyst. Return STRICT JSON only with the following schema: "
            "{\"summary\":\"string\", \"analysis\":\"string\", \"recommendations\": [\"string\"], \"confidence\": "
            "\"number\", \"debug\": {}}. Do NOT include any markdown, lists, or extraneous text."
        )
        try:
            if gemini_sem:
                with gemini_sem:
                    gemini_result = gemini_json_call(
                        client=gemini,
                        model=gemini_model,
                        system_instructions=system_instructions,
                        payload=payload,
                    )
            else:
                gemini_result = gemini_json_call(
                    client=gemini,
                    model=gemini_model,
                    system_instructions=system_instructions,
                    payload=payload,
                )
            # Ensure result is a dict
            if not isinstance(gemini_result, dict):
                gemini_result = {"summary": str(gemini_result), "analysis": None, "recommendations": None}
            new_key_index = current_key_index
        except Exception as e:
            print(f"[{AGENT_NAME}] Gemini JSON call failed: {e}")
            gemini_result = {"summary": None, "analysis": None, "recommendations": None}
            new_key_index = current_key_index

        # Normalize Gemini output: ensure `gemini_result` is a dict with expected keys
        if gemini_result is None or isinstance(gemini_result, str):
            gemini_result = {
                "summary": gemini_result or "",
                "analysis": None,
                "recommendations": None,
            }
        elif isinstance(gemini_result, (list, tuple)):
            # If an LLM helper returned a raw string in a single-item list, flatten it
            if len(gemini_result) == 1 and isinstance(gemini_result[0], str):
                gemini_result = {"summary": gemini_result[0], "analysis": None, "recommendations": None}
            else:
                # convert to a stable dict form
                gemini_result = {"summary": json.dumps(gemini_result), "analysis": None, "recommendations": None}
        
        # Update global key index if rotation occurred
        if new_key_index != current_key_index:
            with key_lock:
                key_index_holder["index"] = new_key_index
            print(f"[{AGENT_NAME}] DEBUG: Gemini key rotated from index {current_key_index} to {new_key_index}")
        print(f"[{AGENT_NAME}] DEBUG: Gemini analysis completed")

        final_payload = {
            "agent": AGENT_NAME,
            "incident_id": incident_id,
            "organization_id": org_id,
            "timestamp": utc_now_iso(),
            "status": "completed",
            "result": gemini_result,
            "debug": {
                "monitor_query": monitor_query,
                "metric_queries": metric_queries,
                "from_ms": from_ms,
                "to_ms": to_ms,
                "snapshot_included": bool(snapshot_bytes),
                "timeseries_count": len(timeseries_data),
                "gemini_key_index_used": new_key_index,
            },
        }

        artifact_paths = write_agent_artifacts(
            incident_id=incident_id,
            agent_name=AGENT_NAME,
            payload=final_payload,
        )
        final_payload["artifacts"] = artifact_paths

        publish_json(producer, result_topic, final_payload)
        print(f"[{AGENT_NAME}] completed incident_id={incident_id}")

    except Exception as e:
        err_str = f"{e}"
        print(f"[{AGENT_NAME}] error processing incident: {err_str}")
        tb = traceback.format_exc()
        print(tb)
        try:
            publish_json(
                producer,
                result_topic,
                {
                    "agent": AGENT_NAME,
                    "incident_id": raw.get("incident_id", ""),
                    "organization_id": raw.get("organization_id", ""),
                    "timestamp": utc_now_iso(),
                    "status": "failed",
                    "error": err_str,
                    "traceback": tb,
                },
            )
        except Exception as e2:
            print(f"[{AGENT_NAME}] failed to publish failure payload: {e2}")


def main() -> None:
    print(f"[{AGENT_NAME}] starting up...")
    
    # Load Datadog site (still needed for API base URL construction)
    dd_site = os.environ.get("DD_SITE", "datadoghq.com")
    print(f"[{AGENT_NAME}] DEBUG: dd_site={dd_site}")

    # Load multiple Gemini API keys for rotation
    gemini_keys = []
    for i in range(1, 10):
        key = os.getenv(f"GEMINI_API_KEY_{i}")
        if key:
            gemini_keys.append(key)
    
    # Fall back to single key if multi-key not configured
    if not gemini_keys:
        single_key = os.getenv("GEMINI_API_KEY")
        if single_key:
            gemini_keys.append(single_key)
    
    if not gemini_keys:
        raise RuntimeError("No GEMINI_API_KEY or GEMINI_API_KEY_* environment variables found")
    
    print(f"[{AGENT_NAME}] loaded {len(gemini_keys)} Gemini API key(s)")
    
    gemini_model = os.getenv("GEMINI_MODEL", "gemini-1.5-flash")
    print(f"[{AGENT_NAME}] DEBUG: gemini_model={gemini_model}")

    client_props_path = os.getenv("CLIENT_PROPERTIES_PATH", "/app/agents/common/client.properties")
    print(f"[{AGENT_NAME}] DEBUG: client_props_path={client_props_path}")

    incidents_topic = "incidents_created"
    started_topic = "agent_started"
    result_topic = "agent_result"

    print(f"[{AGENT_NAME}] DEBUG: Building Kafka config")
    cfg = build_kafka_config(client_props_path, os.environ)
    
    consumer_group = "metrics-agent-v4"
    print(f"[{AGENT_NAME}] DEBUG: Creating consumer with group_id={consumer_group}, auto_offset_reset=latest")
    consumer = make_consumer(cfg, group_id=consumer_group, auto_offset_reset="latest")
    producer = make_producer(cfg)

    try:
        ensure_topics(cfg, [incidents_topic, started_topic, result_topic])
    except Exception as e:
        print(f"[{AGENT_NAME}] ensure_topics failed: {e}")

    print(f"[{AGENT_NAME}] DEBUG: Subscribing to topic={incidents_topic}")
    consumer.subscribe([incidents_topic])

    try:
        gemini = build_gemini_client(gemini_keys[0])
        print(f"[{AGENT_NAME}] DEBUG: Initialized Gemini client with first key")
    except Exception as e:
        raise RuntimeError(f"Failed to initialize Gemini client: {e}")
    
    # Thread-safe key index holder
    key_index_holder = {"index": 0}
    key_lock = threading.Lock()

    # Secrets client to fetch per-organization decrypted keys from backend
    secrets_client = SecretsClient()

    # Concurrency controls
    dd_max = int(os.getenv("DD_MAX_CONCURRENT", "1"))
    gemini_max = int(os.getenv("GEMINI_MAX_CONCURRENT", "2"))
    dd_sem = threading.BoundedSemaphore(dd_max)
    gemini_sem = threading.BoundedSemaphore(gemini_max)
    print(f"[{AGENT_NAME}] DEBUG: dd_max_concurrent={dd_max}, gemini_max_concurrent={gemini_max}")

    print(f"[{AGENT_NAME}] consuming topic={incidents_topic}")
    max_workers = int(os.getenv("AGENT_MAX_WORKERS", "4"))
    print(f"[{AGENT_NAME}] DEBUG: max_workers={max_workers}")
    executor = ThreadPoolExecutor(max_workers=max_workers)
    pending: list[Future] = []
    
    message_count = 0
    start_time = datetime.now(timezone.utc)

    try:
        print(f"[{AGENT_NAME}] DEBUG: Starting consumer poll loop at {start_time}")
        while True:
            msg = consumer.poll(1.0)
            if msg is None:
                pending = [f for f in pending if not f.done()]
                continue
            if msg.error():
                print(f"[{AGENT_NAME}] consumer error: {msg.error()}")
                continue

            message_count += 1
            elapsed_seconds = (datetime.now(timezone.utc) - start_time).total_seconds()
            print(f"[{AGENT_NAME}] DEBUG: Received message #{message_count} (elapsed={elapsed_seconds:.1f}s), offset={msg.offset()}, partition={msg.partition()}")
            
            raw = json.loads(msg.value().decode("utf-8"))

            # Throttle submission if too many pending tasks
            pending = [f for f in pending if not f.done()]
            while sum(1 for f in pending if not f.done()) >= max_workers * 2:
                time.sleep(0.1)
                pending = [f for f in pending if not f.done()]

            future = executor.submit(
                process_incident,
                raw,
                producer,
                gemini,
                dd_site,
                gemini_model,
                started_topic,
                result_topic,
                gemini_keys,
                key_index_holder,
                key_lock,
                dd_sem,
                gemini_sem,
                secrets_client,
            )
            pending.append(future)

    except KeyboardInterrupt:
        print(f"[{AGENT_NAME}] shutting down due to KeyboardInterrupt")
    finally:
        executor.shutdown(wait=True)
        consumer.close()


if __name__ == "__main__":
    main()