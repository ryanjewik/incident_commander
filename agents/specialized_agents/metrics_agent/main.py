from __future__ import annotations
import json
import os
import time
from datetime import datetime, timezone
from typing import Any, Dict
from concurrent.futures import ThreadPoolExecutor, Future
import threading

from agents.common.kafka_client import build_kafka_config, make_consumer, make_producer, ensure_topics
from agents.common.datadog_client import query_timeseries, get_monitor_details, download_snapshot_image
from agents.common.gemini_client import build_gemini_client, analyze_metrics_with_gemini
from agents.common.output_writer import write_agent_artifacts
from agents.common.secrets_client import SecretsClient


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


def process_incident(
    raw: Dict[str, Any],
    producer,
    gemini,
    dd_site: str,
    gemini_model: str,
    started_topic: str,
    result_topic: str,
    gemini_keys: list[str],
    current_key_index: int,
    dd_sem=None,
    gemini_sem=None,
    secrets_client: SecretsClient | None = None,
) -> None:
    try:
        incident_id = raw.get("incident_id", "")
        org_id = raw.get("organization_id", "")
        
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

        # Resolve per-organization Datadog keys (backend-decrypted).
        # Do NOT fall back to environment variables for Datadog API/app keys.
        dd_api_key_used = None
        dd_app_key_used = None
        try:
            if secrets_client and org_id:
                sk = secrets_client.get_datadog_keys(org_id)
                if sk:
                    dd_api_key_used = sk.get("datadog_api_key")
                    dd_app_key_used = sk.get("datadog_app_key")
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

        metric_queries = build_metric_queries(raw, monitor_query)

        timeseries_data = []
        for query in metric_queries:
            if not query:
                continue
            if dd_sem:
                with dd_sem:
                    ts_result = query_timeseries(
                        dd_site=dd_site,
                        dd_api_key=dd_api_key_used,
                        dd_app_key=dd_app_key_used,
                        query=query,
                        from_ms=from_ms,
                        to_ms=to_ms,
                    )
            else:
                ts_result = query_timeseries(
                    dd_site=dd_site,
                    dd_api_key=dd_api_key_used,
                    dd_app_key=dd_app_key_used,
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

        if gemini_sem:
            with gemini_sem:
                gemini_result, new_key_index = analyze_metrics_with_gemini(
                    client=gemini,
                    model=gemini_model,
                    incident_context=incident_context,
                    snapshot_png_bytes=snapshot_bytes,
                    api_keys=gemini_keys,
                    current_key_index=current_key_index,
                )
        else:
            gemini_result, new_key_index = analyze_metrics_with_gemini(
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

    except Exception as e:
        print(f"[{AGENT_NAME}] error processing incident: {e}")


def main() -> None:
    print(f"[{AGENT_NAME}] starting up...")
    
    # Load Datadog site (still needed for API base URL construction)
    dd_site = os.environ.get("DD_SITE", "datadoghq.com")

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

    client_props_path = os.getenv("CLIENT_PROPERTIES_PATH", "/app/agents/common/client.properties")

    incidents_topic = "incidents_created"
    started_topic = "agent_started"
    result_topic = "agent_result"

    cfg = build_kafka_config(client_props_path, os.environ)
    consumer = make_consumer(cfg, group_id="metrics-agent-v3", auto_offset_reset="earliest")
    producer = make_producer(cfg)

    try:
        ensure_topics(cfg, [incidents_topic, started_topic, result_topic])
    except Exception as e:
        print(f"[{AGENT_NAME}] ensure_topics failed: {e}")

    consumer.subscribe([incidents_topic])

    try:
        gemini = build_gemini_client(gemini_keys[0])
    except Exception as e:
        raise RuntimeError(f"Failed to initialize Gemini client: {e}")
    
    current_key_index = 0

    # Secrets client to fetch per-organization decrypted keys from backend
    secrets_client = SecretsClient()

    # Concurrency controls
    dd_max = int(os.getenv("DD_MAX_CONCURRENT", "1"))
    gemini_max = int(os.getenv("GEMINI_MAX_CONCURRENT", "2"))
    dd_sem = threading.BoundedSemaphore(dd_max)
    gemini_sem = threading.BoundedSemaphore(gemini_max)

    print(f"[{AGENT_NAME}] consuming topic={incidents_topic}")
    max_workers = int(os.getenv("AGENT_MAX_WORKERS", "4"))
    executor = ThreadPoolExecutor(max_workers=max_workers)
    pending: list[Future] = []

    try:
        while True:
            msg = consumer.poll(1.0)
            if msg is None:
                pending = [f for f in pending if not f.done()]
                continue
            if msg.error():
                print(f"[{AGENT_NAME}] consumer error: {msg.error()}")
                continue

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
                current_key_index,
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