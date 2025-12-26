#!/usr/bin/env python3
"""
Create Datadog monitors for Redis memory and Gemini metrics.
Reads DD_API_KEY, DD_APP_KEY and DD_SITE from environment or .env.
"""
import os
import requests
import json

# Toggle pruning of duplicate monitors. Set PRUNE_DUPLICATE_MONITORS=false to only report.
PRUNE_DUPLICATE_MONITORS = os.environ.get("PRUNE_DUPLICATE_MONITORS", "true").lower() in ("1", "true", "yes")

DD_API_KEY = os.environ.get("DD_API_KEY")
DD_APP_KEY = os.environ.get("DD_APP_KEY")
DD_SITE = os.environ.get("DD_SITE", "datadoghq.com")

if not DD_API_KEY or not DD_APP_KEY:
    raise SystemExit("DD_API_KEY and DD_APP_KEY must be set in the environment")

base = f"https://api.{DD_SITE}/api/v1"
headers = {"DD-API-KEY": DD_API_KEY, "DD-APPLICATION-KEY": DD_APP_KEY, "Content-Type": "application/json"}

monitors = []

# Redis used memory monitor (service:redis)
# Allow tuning via `REDIS_MEM_THRESHOLD` (bytes). Default raised for demo containers to avoid noisy alerts.
redis_threshold = int(os.environ.get("REDIS_MEM_THRESHOLD", str(50 * 1024 * 1024)))
redis_query = f"avg(last_5m):max:redis.used_memory_bytes{{service:redis}} > {redis_threshold}"
monitors.append({
    "name": "Dummy Redis high memory (simulated)",
    "type": "metric alert",
    "query": redis_query,
    "message": f"Redis used_memory_bytes exceeded threshold (simulated by dummy_app). Threshold={redis_threshold}",
    "tags": ["integration:redisdb", "service:redis", "env:demo"],
    "options": {"notify_audit": False, "locked": False, "timeout_h": 0, "notify_no_data": True, "no_data_timeframe": 2}
})

# Gemini error monitor
gemini_err_query = "sum(last_5m):sum:dummy.gemini.error{*} > 0"
monitors.append({
    "name": "Dummy Gemini errors",
    "type": "metric alert",
    "query": gemini_err_query,
    "message": "dummy.gemini.error detected (simulated by dummy_app).",
    "tags": ["service:gemini", "env:demo"],
    "options": {"notify_audit": False, "locked": False, "timeout_h": 0, "notify_no_data": False}
})

# Gemini latency monitor
# Allow tuning via `GEMINI_LATENCY_THRESHOLD_MS` env var. Raised default to
# avoid noisy alerts in demo environments.
gemini_latency_threshold = int(os.environ.get("GEMINI_LATENCY_THRESHOLD_MS", "5000"))
# Use a shorter evaluation window for latency so spikes recover quickly
monitor_window = os.environ.get('MONITOR_WINDOW', '1m')
gemini_lat_query = f"avg(last_{monitor_window}):avg:dummy.gemini.duration_ms{{*}} > {gemini_latency_threshold}"
monitors.append({
    "name": "Dummy Gemini high latency",
    "type": "metric alert",
    "query": gemini_lat_query,
    "message": f"High Gemini latency observed (simulated by dummy_app). Threshold={gemini_latency_threshold}ms",
    "tags": ["service:gemini", "env:demo"],
    "options": {"notify_audit": False, "locked": False, "timeout_h": 0, "notify_no_data": False}
})

# System host monitors (ensure they exist and use 1-minute window for fast recovery)
cpu_threshold = int(os.environ.get('SYSTEM_CPU_THRESHOLD', '10'))
mem_threshold = int(os.environ.get('SYSTEM_MEM_THRESHOLD', '30'))
cpu_query = f"avg(last_{monitor_window}):avg:dummy.system.cpu_percent{{env:demo}} > {cpu_threshold}"
mem_query = f"avg(last_{monitor_window}):avg:dummy.system.memory_percent{{env:demo}} > {mem_threshold}"
monitors.append({
    "name": "Simulated Dummy system CPU high",
    "type": "metric alert",
    "query": cpu_query,
    "message": "Simulated CPU high",
    "tags": ["env:demo"],
    "options": {"notify_audit": False, "locked": False, "timeout_h": 0, "notify_no_data": False}
})
monitors.append({
    "name": "Simulated Dummy system memory high",
    "type": "metric alert",
    "query": mem_query,
    "message": "Simulated memory high",
    "tags": ["env:demo"],
    "options": {"notify_audit": False, "locked": False, "timeout_h": 0, "notify_no_data": False}
})

def get_all_monitors():
    resp = requests.get(f"{base}/monitor", headers=headers, params={"page": 0})
    resp.raise_for_status()
    return resp.json()


def delete_monitor(mid):
    resp = requests.delete(f"{base}/monitor/{mid}", headers=headers)
    if resp.status_code == 200:
        print(f"Deleted duplicate monitor: {mid}")
    else:
        print(f"Failed to delete monitor {mid}:", resp.status_code, resp.text)


def update_monitor(mid, payload):
    resp = requests.put(f"{base}/monitor/{mid}", headers=headers, data=json.dumps(payload))
    resp.raise_for_status()
    rj = resp.json()
    print("Updated monitor:", rj.get("id"), rj.get("name"))
    return rj


def create_monitor(payload):
    resp = requests.post(f"{base}/monitor", headers=headers, data=json.dumps(payload))
    resp.raise_for_status()
    rj = resp.json()
    print("Created monitor:", rj.get("id"), rj.get("name"))
    return rj


existing = get_all_monitors()
by_name = {}
for em in existing:
    name = em.get("name")
    by_name.setdefault(name, []).append(em)

created = []
updated = []
deleted = []

for m in monitors:
    name = m.get("name")
    if name in by_name and by_name[name]:
        entries = by_name[name]
        keeper = entries[0]
        if len(entries) > 1:
            for dup in entries[1:]:
                if PRUNE_DUPLICATE_MONITORS:
                    delete_monitor(dup.get("id"))
                    deleted.append(dup.get("id"))
                else:
                    print(f"Found duplicate monitor (not pruned): {dup.get('id')} - {name}")
        try:
            rj = update_monitor(keeper.get("id"), m)
            updated.append((rj.get("id"), rj.get("name")))
        except Exception as e:
            print(f"Failed to update existing monitor {keeper.get('id')}: {e}")
    else:
        try:
            rj = create_monitor(m)
            created.append((rj.get("id"), rj.get("name")))
        except Exception as e:
            print(f"Failed to create monitor {name}: {e}")

print("Done.")
if created:
    print("Created monitors:")
    for c in created:
        print(c)
if updated:
    print("Updated monitors:")
    for u in updated:
        print(u)
if deleted:
    print("Deleted monitor IDs:")
    for d in deleted:
        print(d)
