from __future__ import annotations
import json
import os
import time
from datetime import datetime, timezone
from typing import Any, Dict

from kafka_client import build_kafka_config, make_consumer, make_producer, ensure_topics
from datadog_client import (
    parse_related_logs_url_time_window,
    search_logs,
    aggregate_logs,
    search_spans,
    download_snapshot_image,
)
from gemini_client import build_gemini_client, analyze_incident_with_gemini
from output_writer import write_agent_artifacts


AGENT_NAME = "logs_traces_agent"


def utc_now_iso() -> str:
    return datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")


def publish_json(producer, topic: str, payload: Dict[str, Any]) -> None:
    try:
        producer.produce(topic, json.dumps(payload).encode("utf-8"))
        producer.flush(10)
    except Exception as e:
        # Log produce errors and continue so agent doesn't crash on missing topics
        print(f"[{AGENT_NAME}] failed to publish to topic={topic}: {e}")


def build_log_query(raw: Dict[str, Any]) -> str:
    # Prefer the tags in the payload if present
    tags = raw.get("raw_payload_ref", {}).get("event", {}).get("tags", "") or ""
    # Example tags: "env:demo,integration:redisdb,monitor,service:redis"
    # Convert commas to spaces for Datadog query syntax but ignore bare tokens
    # (like 'monitor') which often are not useful in log searches and can
    # accidentally filter out results. Only include key:value tokens.
    tag_parts = [t.strip() for t in tags.split(",") if t.strip()]
    tag_query = " ".join(p for p in tag_parts if ":" in p)

    # Fallback to service extracted
    service = None
    for t in tags.split(","):
        if t.strip().startswith("service:"):
            service = t.strip()
            break

    if tag_query:
        return tag_query
    if service:
        return service
    return ""


def build_span_query(raw: Dict[str, Any]) -> str:
    # Usually service-based is enough to start; you can extend to env, resource, etc.
    tags = raw.get("raw_payload_ref", {}).get("event", {}).get("tags", "") or ""
    service = None
    for t in tags.split(","):
        if t.strip().startswith("service:"):
            service = t.strip().split(":", 1)[1]
            break
    if service:
        return f"service:{service}"
    return ""


def main() -> None:
    # --- Env ---
    dd_api_key = os.environ["DD_API_KEY"]
    dd_app_key = os.environ["DD_APP_KEY"]
    dd_site = os.environ["DD_SITE"]  # e.g. us5.datadoghq.com

    gemini_key = os.environ["GEMINI_API_KEY"]
    gemini_model = os.getenv("GEMINI_MODEL", "gemini-2.0-flash")

    client_props_path = os.getenv("CLIENT_PROPERTIES_PATH", "client.properties")

    incidents_topic = "incidents_created"
    started_topic = "agent_started"
    result_topic = "agent_result"

    # --- Kafka setup ---
    cfg = build_kafka_config(client_props_path, os.environ)
    consumer = make_consumer(cfg, group_id="logs-traces-agent-v1")
    producer = make_producer(cfg)

    # Ensure the topics we will consume/produce exist (best-effort)
    try:
        ensure_topics(cfg, [incidents_topic, started_topic, result_topic])
    except Exception as e:
        print(f"[{AGENT_NAME}] ensure_topics failed: {e}")

    consumer.subscribe([incidents_topic])

    gemini = build_gemini_client(gemini_key)

    print(f"[{AGENT_NAME}] consuming topic={incidents_topic}")
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
            ts = raw.get("timestamp", utc_now_iso())

            # --- publish started ---
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

            # --- time window ---
            text_only = raw.get("raw_payload_ref", {}).get("event", {}).get("text_only_message", "") or ""
            window = parse_related_logs_url_time_window(text_only)

            # fallback: center around event last_updated_ms
            last_updated_ms = int(raw.get("raw_payload_ref", {}).get("event", {}).get("last_updated_ms", "0") or "0")
            if window:
                from_ms, to_ms = window
            else:
                # Default: last_updated - 15m to last_updated + 5m
                from_ms = max(0, last_updated_ms - 15 * 60 * 1000)
                to_ms = last_updated_ms + 5 * 60 * 1000

            # --- build queries ---
            log_query = build_log_query(raw)
            span_query = build_span_query(raw)

            # --- fetch logs ---
            logs = search_logs(
                dd_site=dd_site,
                dd_api_key=dd_api_key,
                dd_app_key=dd_app_key,
                query=log_query,
                from_ms=from_ms,
                to_ms=to_ms,
                limit=50,
            )

            # If no logs found, try progressively relaxed queries so we don't
            # prematurely conclude there are zero logs. Record which query
            # succeeded in the debug section.
            logs_query_used = log_query
            if not logs:
                # derive env and service from tags/raw payload as fallbacks
                raw_tags = raw.get("raw_payload_ref", {}).get("event", {}).get("tags", "") or ""
                env_val = None
                svc_val = None
                for t in (p.strip() for p in raw_tags.split(",") if p.strip()):
                    if t.startswith("env:"):
                        env_val = t.split(":", 1)[1]
                    if t.startswith("service:"):
                        svc_val = t.split(":", 1)[1]

                fallback_queries = []
                if svc_val and env_val:
                    fallback_queries.append(f"env:{env_val} service:{svc_val}")
                if svc_val:
                    fallback_queries.append(f"service:{svc_val}")
                if env_val:
                    fallback_queries.append(f"env:{env_val}")
                fallback_queries.append("")  # global (no filter)

                for fq in fallback_queries:
                    try:
                        candidate = search_logs(
                            dd_site=dd_site,
                            dd_api_key=dd_api_key,
                            dd_app_key=dd_app_key,
                            query=fq,
                            from_ms=from_ms,
                            to_ms=to_ms,
                            limit=100,
                        )
                    except Exception as e:
                        print(f"[{AGENT_NAME}] fallback search error for query={fq}: {e}")
                        candidate = []
                    if candidate:
                        logs = candidate
                        logs_query_used = fq
                        break


            # pre-aggregate logs: top services + top hosts
            logs_top_service = aggregate_logs(
                dd_site=dd_site,
                dd_api_key=dd_api_key,
                dd_app_key=dd_app_key,
                query=log_query,
                from_ms=from_ms,
                to_ms=to_ms,
                group_by="service",
                limit=10,
            )
            logs_top_host = aggregate_logs(
                dd_site=dd_site,
                dd_api_key=dd_api_key,
                dd_app_key=dd_app_key,
                query=log_query,
                from_ms=from_ms,
                to_ms=to_ms,
                group_by="host",
                limit=10,
            )

            # --- fetch spans (traces) ---
            spans = []
            if span_query:
                spans = search_spans(
                    dd_site=dd_site,
                    dd_api_key=dd_api_key,
                    dd_app_key=dd_app_key,
                    query=span_query,
                    from_ms=from_ms,
                    to_ms=to_ms,
                    limit=50,
                )

            # --- snapshot image ---
            snapshot_url = raw.get("raw_payload_ref", {}).get("event", {}).get("snapshot_url", "") or ""
            snapshot_bytes = None
            if snapshot_url:
                try:
                    snapshot_bytes = download_snapshot_image(snapshot_url)
                except Exception as e:
                    print(f"[{AGENT_NAME}] snapshot download failed: {e}")

            # --- build condensed context for Gemini ---
            event = raw.get("raw_payload_ref", {}).get("event", {}) or {}
            monitor = raw.get("raw_payload_ref", {}).get("monitor", {}) or {}

            incident_context: Dict[str, Any] = {
                "incident_id": incident_id,
                "organization_id": org_id,
                "datadog_site": dd_site,
                "time_window_ms": {"from": from_ms, "to": to_ms},
                "monitor": {
                    "id": raw.get("monitor_id"),
                    "query": monitor.get("query"),
                    "title": monitor.get("alert_title") or event.get("title"),
                    "tags": event.get("tags"),
                    "event_url": event.get("link"),
                },
                "snapshot_url": snapshot_url,
                "logs_query": log_query,
                "spans_query": span_query,
                "logs_aggregations": {
                    "top_service": logs_top_service,
                    "top_host": logs_top_host,
                },
                "log_samples": logs[:15],   # keep small
                "span_samples": spans[:15], # keep small
            }

            gemini_result = analyze_incident_with_gemini(
                client=gemini,
                model=gemini_model,
                incident_context=incident_context,
                snapshot_png_bytes=snapshot_bytes,
            )

            final_payload = {
                "agent": AGENT_NAME,
                "incident_id": incident_id,
                "organization_id": org_id,
                "timestamp": utc_now_iso(),
                "status": "completed",
                "result": gemini_result,
                "debug": {
                    "dd_logs_query": log_query,
                    "dd_logs_query_used": logs_query_used,
                    "dd_spans_query": span_query,
                    "from_ms": from_ms,
                    "to_ms": to_ms,
                    "snapshot_included": bool(snapshot_bytes),
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

    finally:
        consumer.close()


if __name__ == "__main__":
    main()
