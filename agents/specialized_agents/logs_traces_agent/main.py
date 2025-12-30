from __future__ import annotations
import json
import os
import time
from datetime import datetime, timezone
from typing import Any, Dict
import traceback
import hashlib

from agents.common.kafka_client import build_kafka_config, make_consumer, make_producer, ensure_topics
from agents.common.datadog_client import (
    parse_related_logs_url_time_window,
    search_logs,
    aggregate_logs,
    search_spans,
    download_snapshot_image,
)
from agents.common.gemini_client import build_gemini_client, gemini_json_call
from agents.common.output_writer import write_agent_artifacts
from concurrent.futures import ThreadPoolExecutor, Future
import threading
from agents.common.secrets_client import SecretsClient


AGENT_NAME = "logs_traces_agent"


def utc_now_iso() -> str:
    return datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")


def publish_json(producer, topic: str, payload: Dict[str, Any]) -> None:
    try:
        producer.produce(topic, json.dumps(payload).encode("utf-8"))
        producer.flush(10)
        try:
            # lightweight publish log (avoid dumping full payload)
            print(f"[{AGENT_NAME}] published topic={topic} agent={payload.get('agent')} incident={payload.get('incident_id')} status={payload.get('status')}")
        except Exception:
            pass
    except Exception as e:
        print(f"[{AGENT_NAME}] failed to publish to topic={topic}: {e}")


def build_log_query(raw: Dict[str, Any]) -> str:
    tags = raw.get("raw_payload_ref", {}).get("event", {}).get("tags", "") or ""
    tag_parts = [t.strip() for t in tags.split(",") if t.strip()]
    tag_query = " ".join(p for p in tag_parts if ":" in p)

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
    tags = raw.get("raw_payload_ref", {}).get("event", {}).get("tags", "") or ""
    service = None
    for t in tags.split(","):
        if t.strip().startswith("service:"):
            service = t.strip().split(":", 1)[1]
            break
    if service:
        return f"service:{service}"
    return ""


def process_incident(
    raw: Dict[str, Any],
    producer,
    gemini,
    dd_site: str,
    gemini_model: str,
    started_topic: str,
    result_topic: str,
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

        text_only = raw.get("raw_payload_ref", {}).get("event", {}).get("text_only_message", "") or ""
        window = parse_related_logs_url_time_window(text_only)

        last_updated_ms = int(raw.get("raw_payload_ref", {}).get("event", {}).get("last_updated_ms", "0") or "0")
        if window:
            from_ms, to_ms = window
        else:
            from_ms = max(0, last_updated_ms - 15 * 60 * 1000)
            to_ms = last_updated_ms + 5 * 60 * 1000

        log_query = build_log_query(raw)
        span_query = build_span_query(raw)

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
                    print(f"[{AGENT_NAME}] fetched secrets for org={org_id} api_key_present={bool(dd_api_key_used)} app_key_present={bool(dd_app_key_used)}")
                    try:
                        api_fp = hashlib.sha256((dd_api_key_used or "").encode()).hexdigest()[:12]
                        app_fp = hashlib.sha256((dd_app_key_used or "").encode()).hexdigest()[:12]
                        print(f"[{AGENT_NAME}] datadog debug dd_site={dd_site} api_base=https://api.{dd_site} api_len={len(dd_api_key_used or '')} app_len={len(dd_app_key_used or '')} api_sha256={api_fp} app_sha256={app_fp}")
                    except Exception:
                        pass
        except Exception as e:
            print(f"[{AGENT_NAME}] secrets fetch error for org={org_id}: {e}")
            traceback.print_exc()

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

        if dd_sem:
            with dd_sem:
                logs = search_logs(
                    dd_site=dd_site,
                    dd_api_key=dd_api_key_used,
                    dd_app_key=dd_app_key_used,
                    query=log_query,
                    from_ms=from_ms,
                    to_ms=to_ms,
                    limit=50,
                )
        else:
            logs = search_logs(
                dd_site=dd_site,
                dd_api_key=dd_api_key_used,
                dd_app_key=dd_app_key_used,
                query=log_query,
                from_ms=from_ms,
                to_ms=to_ms,
                limit=50,
            )

        logs_query_used = log_query
        if not logs:
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
            fallback_queries.append("")

            for fq in fallback_queries:
                try:
                    if dd_sem:
                        with dd_sem:
                            candidate = search_logs(
                                dd_site=dd_site,
                                dd_api_key=dd_api_key_used,
                                dd_app_key=dd_app_key_used,
                                query=fq,
                                from_ms=from_ms,
                                to_ms=to_ms,
                                limit=100,
                            )
                    else:
                        candidate = search_logs(
                            dd_site=dd_site,
                            dd_api_key=dd_api_key_used,
                            dd_app_key=dd_app_key_used,
                            query=fq,
                            from_ms=from_ms,
                            to_ms=to_ms,
                            limit=100,
                        )
                except Exception as e:
                    print(f"[{AGENT_NAME}] fallback search error for query={fq}: {e}")
                    traceback.print_exc()
                    candidate = []
                if candidate:
                    logs = candidate
                    logs_query_used = fq
                    print(f"[{AGENT_NAME}] fallback query succeeded fq={fq} results={len(candidate)}")
                    break
            if not logs:
                print(f"[{AGENT_NAME}] no logs found for incident={incident_id} using queries={fallback_queries}")

        if dd_sem:
            with dd_sem:
                logs_top_service = aggregate_logs(
                    dd_site=dd_site,
                    dd_api_key=dd_api_key_used,
                    dd_app_key=dd_app_key_used,
                    query=log_query,
                    from_ms=from_ms,
                    to_ms=to_ms,
                    group_by="service",
                    limit=10,
                )
                logs_top_host = aggregate_logs(
                    dd_site=dd_site,
                    dd_api_key=dd_api_key_used,
                    dd_app_key=dd_app_key_used,
                    query=log_query,
                    from_ms=from_ms,
                    to_ms=to_ms,
                    group_by="host",
                    limit=10,
                )
        else:
            logs_top_service = aggregate_logs(
                dd_site=dd_site,
                dd_api_key=dd_api_key_used,
                dd_app_key=dd_app_key_used,
                query=log_query,
                from_ms=from_ms,
                to_ms=to_ms,
                group_by="service",
                limit=10,
            )
            logs_top_host = aggregate_logs(
                dd_site=dd_site,
                dd_api_key=dd_api_key_used,
                dd_app_key=dd_app_key_used,
                query=log_query,
                from_ms=from_ms,
                to_ms=to_ms,
                group_by="host",
                limit=10,
            )

        spans = []
        if span_query:
            if dd_sem:
                with dd_sem:
                    spans = search_spans(
                        dd_site=dd_site,
                        dd_api_key=dd_api_key_used,
                        dd_app_key=dd_app_key_used,
                        query=span_query,
                        from_ms=from_ms,
                        to_ms=to_ms,
                        limit=50,
                    )
            else:
                spans = search_spans(
                    dd_site=dd_site,
                    dd_api_key=dd_api_key_used,
                    dd_app_key=dd_app_key_used,
                    query=span_query,
                    from_ms=from_ms,
                    to_ms=to_ms,
                    limit=50,
                )

        snapshot_url = raw.get("raw_payload_ref", {}).get("event", {}).get("snapshot_url", "") or ""
        snapshot_bytes = None
        if snapshot_url:
            try:
                snapshot_bytes = download_snapshot_image(snapshot_url)
            except Exception as e:
                print(f"[{AGENT_NAME}] snapshot download failed: {e}")

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
            "logs_aggregations": {"top_service": logs_top_service, "top_host": logs_top_host},
            "log_samples": logs[:15],
            "span_samples": spans[:15],
        }

        # Request strict JSON from Gemini to avoid markdown output.
        logs_for_gemini = incident_context.get("log_samples", []) or logs[:15]
        payload = {
            "task": "Analyze logs and traces and return a STRICT JSON report.",
            "incident_context": incident_context,
            "logs": logs_for_gemini,
        }
        system_instructions = (
            "You are an incident analysis assistant. Return STRICT JSON only with the schema: "
            "{\"summary\":\"string\", \"analysis\":\"string\", \"recommendations\": [\"string\"], "
            "\"confidence\": \"number\", \"debug\": {}}. DO NOT include markdown, bullet points, or any text outside the JSON object."
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
            if not isinstance(gemini_result, dict):
                gemini_result = {"summary": str(gemini_result), "analysis": None, "recommendations": None}
        except Exception as e:
            print(f"[{AGENT_NAME}] Gemini JSON call failed: {e}")
            gemini_result = {"summary": None, "analysis": None, "recommendations": None}
        print(f"[{AGENT_NAME}] gemini_result type={type(gemini_result)}")

        # Normalize Gemini output: ensure `gemini_result` becomes a stable dict
        # The helper may return (text, key_index) or raw string; normalize similar to metrics_agent.
        try:
            # If tuple/list like (text, index)
            if isinstance(gemini_result, (list, tuple)):
                if len(gemini_result) == 2 and isinstance(gemini_result[1], int):
                    gemini_text = gemini_result[0]
                    gemini_result = {"summary": gemini_text or "", "analysis": None, "recommendations": None}
                else:
                    # flatten other list/tuple forms
                    gemini_result = {"summary": json.dumps(gemini_result), "analysis": None, "recommendations": None}
            elif gemini_result is None or isinstance(gemini_result, str):
                gemini_result = {"summary": gemini_result or "", "analysis": None, "recommendations": None}
            elif isinstance(gemini_result, dict):
                # keep as-is
                pass
            else:
                gemini_result = {"summary": str(gemini_result), "analysis": None, "recommendations": None}
        except Exception:
            gemini_result = {"summary": str(gemini_result), "analysis": None, "recommendations": None}

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

        artifact_paths = write_agent_artifacts(incident_id=incident_id, agent_name=AGENT_NAME, payload=final_payload)
        final_payload["artifacts"] = artifact_paths
        print(f"[{AGENT_NAME}] wrote artifacts for incident={incident_id}: {artifact_paths}")

        publish_json(producer, result_topic, final_payload)
        print(f"[{AGENT_NAME}] completed incident_id={incident_id}")

    except Exception as e:
        print(f"[{AGENT_NAME}] error processing incident: {e}")
        traceback.print_exc()
        try:
            publish_json(
                producer,
                result_topic,
                {
                    "agent": AGENT_NAME,
                    "incident_id": incident_id,
                    "organization_id": org_id,
                    "timestamp": utc_now_iso(),
                    "status": "failed",
                    "error": str(e),
                },
            )
        except Exception as e2:
            print(f"[{AGENT_NAME}] failed to publish failure result: {e2}")
            traceback.print_exc()


def main() -> None:
    # Prefer per-organization Datadog keys via `SecretsClient`.
    # Use env vars only as a non-fatal fallback for local dev; do not crash at startup if missing.
    # Do not read global Datadog keys from environment; use per-org secrets via SecretsClient only.
    dd_site = os.getenv("DD_SITE", "datadoghq.com")

    gemini_key = os.environ["GEMINI_API_KEY"]
    gemini_model = os.getenv("GEMINI_MODEL", "gemini-2.0-flash")

    client_props_path = os.getenv("CLIENT_PROPERTIES_PATH", "../../common/client.properties")

    incidents_topic = "incidents_created"
    started_topic = "agent_started"
    result_topic = "agent_result"

    cfg = build_kafka_config(client_props_path, os.environ)
    consumer = make_consumer(cfg, group_id="logs-traces-agent-v1")
    producer = make_producer(cfg)

    try:
        ensure_topics(cfg, [incidents_topic, started_topic, result_topic])
    except Exception as e:
        print(f"[{AGENT_NAME}] ensure_topics failed: {e}")

    consumer.subscribe([incidents_topic])

    gemini = build_gemini_client(gemini_key)

    dd_max = int(os.getenv("DD_MAX_CONCURRENT", "1"))
    gemini_max = int(os.getenv("GEMINI_MAX_CONCURRENT", "2"))
    dd_sem = threading.BoundedSemaphore(dd_max)
    gemini_sem = threading.BoundedSemaphore(gemini_max)

    # Secrets client to fetch per-organization decrypted keys from backend
    secrets_client = SecretsClient()

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
