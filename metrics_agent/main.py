from __future__ import annotations
import json
import os
from datetime import datetime, timezone
from typing import Any, Dict

from kafka_client import build_kafka_config, make_consumer, make_producer, ensure_topics
from datadog_client import query_timeseries, get_monitor_details, download_snapshot_image
from gemini_client import build_gemini_client, analyze_metrics_with_gemini
from output_writer import write_agent_artifacts


AGENT_NAME = "metrics_agent"


def utc_now_iso() -> str:
    return datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")


def publish_json(producer, topic: str, payload: Dict[str, Any]) -> None:
    try:
        producer.produce(topic, json.dumps(payload).encode("utf-8"))
        producer.flush(10)
    except Exception as e:
        print(f"[{AGENT_NAME}] failed to publish to topic={topic}: {e}")


def build_metric_queries(raw: Dict[str, Any], monitor_query: str) -> list[str]:
    queries = []
    if monitor_query:
        queries.append(monitor_query)
    
    tags = raw.get("raw_payload_ref", {}).get("event", {}).get("tags", "") or ""
    service = None
    env = None
    for t in tags.split(","):
        t = t.strip()
        if t.startswith("service:"):
            service = t.split(":", 1)[1]
        if t.startswith("env:"):
            env = t.split(":", 1)[1]
    
    if service and env:
        queries.append(f"avg:system.cpu.user{{service:{service},env:{env}}}")
        queries.append(f"avg:system.mem.used{{service:{service},env:{env}}}")
        queries.append(f"avg:system.load.1{{service:{service},env:{env}}}")
    elif service:
        queries.append(f"avg:system.cpu.user{{service:{service}}}")
        queries.append(f"avg:system.mem.used{{service:{service}}}")
    
    return queries


def main() -> None:
    print(f"[{AGENT_NAME}] starting up...")
    
    # Validate required environment variables
    required_vars = ["DD_API_KEY", "DD_APP_KEY", "DD_SITE"]
    missing_vars = [var for var in required_vars if not os.getenv(var)]
    if missing_vars:
        raise RuntimeError(f"Missing required environment variables: {', '.join(missing_vars)}")
    
    dd_api_key = os.environ["DD_API_KEY"]
    dd_app_key = os.environ["DD_APP_KEY"]
    dd_site = os.environ["DD_SITE"]

    gemini_keys = []
    for i in range(1, 10):
        key = os.getenv(f"GEMINI_API_KEY_{i}")
        if key:
            gemini_keys.append(key)
    
    if not gemini_keys:
        raise RuntimeError("No GEMINI_API_KEY_* environment variables found")
    
    print(f"[{AGENT_NAME}] loaded {len(gemini_keys)} Gemini API keys")
    
    gemini_model = os.getenv("GEMINI_MODEL", "gemini-1.5-flash")

    client_props_path = os.getenv("CLIENT_PROPERTIES_PATH", "client.properties")

    incidents_topic = "incidents_created"
    started_topic = "agent_started"
    result_topic = "agent_result"

    cfg = build_kafka_config(client_props_path, os.environ)
    consumer = make_consumer(cfg, group_id="metrics-agent-v1")
    producer = make_producer(cfg)

    try:
        ensure_topics(cfg, [incidents_topic, started_topic, result_topic])
    except Exception as e:
        print(f"[{AGENT_NAME}] ensure_topics failed: {e}")

    consumer.subscribe([incidents_topic])

    try:
        gemini = build_gemini_client(gemini_keys[0])
        print(f"[{AGENT_NAME}] Gemini client initialized successfully")
    except Exception as e:
        raise RuntimeError(f"Failed to initialize Gemini client: {e}")
    
    current_key_index = 0

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

            last_updated_ms = int(raw.get("raw_payload_ref", {}).get("event", {}).get("last_updated_ms", "0") or "0")
            from_ms = max(0, last_updated_ms - 30 * 60 * 1000)
            to_ms = last_updated_ms + 5 * 60 * 1000

            monitor_id_str = raw.get("monitor_id", "")
            monitor_id = None
            if isinstance(monitor_id_str, str) and monitor_id_str.isdigit():
                monitor_id = int(monitor_id_str)

            monitor_details = {}
            monitor_query = ""
            if monitor_id:
                monitor_details = get_monitor_details(
                    dd_site=dd_site,
                    dd_api_key=dd_api_key,
                    dd_app_key=dd_app_key,
                    monitor_id=monitor_id,
                )
                monitor_query = monitor_details.get("query", "")

            metric_queries = build_metric_queries(raw, monitor_query)

            timeseries_data = []
            for query in metric_queries:
                if not query:
                    continue
                ts_result = query_timeseries(
                    dd_site=dd_site,
                    dd_api_key=dd_api_key,
                    dd_app_key=dd_app_key,
                    query=query,
                    from_ms=from_ms,
                    to_ms=to_ms,
                )
                if ts_result:
                    timeseries_data.append({"query": query, "result": ts_result})

            snapshot_url = raw.get("raw_payload_ref", {}).get("event", {}).get("snapshot_url", "") or ""
            snapshot_bytes = None
            if snapshot_url:
                try:
                    snapshot_bytes = download_snapshot_image(snapshot_url)
                except Exception as e:
                    print(f"[{AGENT_NAME}] snapshot download failed: {e}")

            event = raw.get("raw_payload_ref", {}).get("event", {}) or {}

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
            }

            gemini_result, current_key_index = analyze_metrics_with_gemini(
                client=gemini,
                model=gemini_model,
                incident_context=incident_context,
                snapshot_png_bytes=snapshot_bytes,
                api_keys=gemini_keys,
                current_key_index=current_key_index,
            )

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
