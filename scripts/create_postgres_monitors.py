#!/usr/bin/env python3
"""
Create Datadog monitors for Postgres metrics and logs using credentials from environment/.env.
"""
import os
import requests
import json
import time

# Load .env file if present so running the script picks up local env defaults
def load_dotenv_file(path='.env'):
    try:
        if not os.path.exists(path):
            return
        with open(path, 'r') as f:
            for line in f:
                line = line.strip()
                if not line or line.startswith('#'):
                    continue
                if '=' not in line:
                    continue
                k, v = line.split('=', 1)
                k = k.strip()
                v = v.strip().strip('"').strip("'")
                # do not overwrite already-set env vars
                os.environ.setdefault(k, v)
    except Exception:
        pass

load_dotenv_file()

# Small helper to toggle deleting duplicates. Set to 'false' to only report duplicates.
PRUNE_DUPLICATE_MONITORS = os.environ.get("PRUNE_DUPLICATE_MONITORS", "true").lower() in ("1", "true", "yes")

DD_API_KEY = os.environ.get("DD_API_KEY")
DD_APP_KEY = os.environ.get("DD_APP_KEY")
DD_SITE = os.environ.get("DD_SITE", "datadoghq.com")

if not DD_API_KEY or not DD_APP_KEY:
    raise SystemExit("DD_API_KEY and DD_APP_KEY must be set in the environment")

base = f"https://api.{DD_SITE}/api/v1"
headers = {"DD-API-KEY": DD_API_KEY, "DD-APPLICATION-KEY": DD_APP_KEY, "Content-Type": "application/json"}

monitors = []

# Postgres connectivity (service_check)
monitors.append({
    "name": "Dummy Postgres connectivity (simulated)",
    "type": "service check",
    "query": "postgres.can_connect",
    "message": "Postgres connectivity issues detected (simulated by dummy_app).",
    "tags": ["service:postgres", "env:demo"],
    "options": {"notify_audit": False, "locked": False, "timeout_h": 0, "notify_no_data": False}
})

# Postgres low connections (indicates DB down)
# Allow configuring the low-connection threshold via `PG_CONN_THRESHOLD` env var
pg_conn_threshold = float(os.environ.get('PG_CONN_THRESHOLD', '1'))
pg_conn_query = f"avg(last_5m):avg:pg.numbackends{{service:postgres,env:demo}} < {pg_conn_threshold}"
monitors.append({
    "name": "Dummy Postgres low connections (simulated)",
    "type": "metric alert",
    "query": pg_conn_query,
    "message": "Postgres has very few backends (possible outage) (simulated by dummy_app).",
    "tags": ["service:postgres", "env:demo"],
    "options": {"notify_audit": False, "locked": False, "timeout_h": 0, "notify_no_data": False}
})

# Postgres rollbacks (unexpected activity)
# Allow tuning the alert threshold via environment variable `PG_ROLLBACK_THRESHOLD`.
# Raise the default in demo mode to reduce noisy alerts; override with env var if desired.
rollback_threshold = int(os.environ.get("PG_ROLLBACK_THRESHOLD", "20"))
pg_rollback_query = f"sum(last_5m):sum:pg.xact_rollback{{service:postgres,env:demo}} > {rollback_threshold}"
monitors.append({
    "name": "Dummy Postgres rollbacks (simulated)",
    "type": "metric alert",
    "query": pg_rollback_query,
    "message": f"Postgres reported transaction rollbacks (simulated by dummy_app). Threshold={rollback_threshold}",
    "tags": ["service:postgres", "env:demo"],
    "options": {"notify_audit": False, "locked": False, "timeout_h": 0, "notify_no_data": False}
})

# Failed inserts emitted by the app
pg_insert_fail_query = "sum(last_5m):sum:dummy.pg.insert.failed{service:postgres} > 0"
monitors.append({
    "name": "Dummy Postgres failed inserts",
    "type": "metric alert",
    "query": pg_insert_fail_query,
    "message": "Application failed to insert into Postgres (simulated by dummy_app).",
    "tags": ["service:postgres", "env:demo"],
    "options": {"notify_audit": False, "locked": False, "timeout_h": 0, "notify_no_data": False}
})

# Log-based monitor: Postgres errors in logs (common pattern)
# This creates a log monitor that looks for "ERROR" lines in Postgres logs.
log_error_threshold = int(os.environ.get("LOG_ERROR_THRESHOLD", "10"))
# Require multiple error log lines within the last 5m before alerting (reduces noise)
# If `PG_EMIT_ZERO_LOG_ERRORS` is enabled we emit a metric `pg.log_errors`
# (value 0) and prefer using a metric-based monitor which can immediately
# observe zero-valued samples. Switch based on env to allow graceful cutover.
if os.environ.get("PG_EMIT_ZERO_LOG_ERRORS", "false").lower() in ("1", "true", "yes"):
    # metric-based fallback: sum of pg.log_errors over last 5m
    log_query = f"sum(last_5m):sum:pg.log_errors{{service:postgres,env:demo}} > {log_error_threshold}"
else:
    log_query = f'logs("service:postgres AND status:error OR message:ERROR").index("*").rollup("count").last("5m") > {log_error_threshold}'
log_monitor_type = "metric alert" if os.environ.get("PG_EMIT_ZERO_LOG_ERRORS", "false").lower() in ("1", "true", "yes") else "log alert"
monitors.append({
    "name": "Dummy Postgres log errors",
    "type": log_monitor_type,
    "query": log_query,
    "message": f"Errors detected in Postgres logs (simulated). Threshold={log_error_threshold}",
    "tags": ["service:postgres", "env:demo"],
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
    # Special-case: if we are switching the log-errors monitor to a metric
    # (because `PG_EMIT_ZERO_LOG_ERRORS=true`), delete any existing log
    # monitor first and create the new metric-based one. Datadog's API may
    # reject in-place type changes, so recreate to be safe.
    if name == "Dummy Postgres log errors" and os.environ.get("PG_EMIT_ZERO_LOG_ERRORS", "false").lower() in ("1","true","yes"):
        if name in by_name and by_name[name]:
            for em in by_name[name]:
                try:
                    delete_monitor(em.get("id"))
                    deleted.append(em.get("id"))
                except Exception as e:
                    print(f"Failed to delete existing log-errors monitor {em.get('id')}: {e}")
            # refresh existing map so we fall through to creation path
            by_name.pop(name, None)

    if name in by_name and by_name[name]:
        # If duplicates exist, delete extras (keep the first), then update the keeper.
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
