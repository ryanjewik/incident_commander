import os
import time
import random
import sqlite3
import logging
import uuid
from datetime import datetime

from fastapi import FastAPI, HTTPException, Request
from pydantic import BaseModel

from datadog import statsd, initialize
import socket
import os as _os
import threading
import requests
import psutil
import gc
try:
    from dotenv import load_dotenv
    load_dotenv()
except Exception:
    pass

try:
    from ddtrace import tracer, patch_all, __version__ as ddtrace_version
    from ddtrace.contrib.asgi import TraceMiddleware
except Exception:
    tracer = None
    patch_all = None
    TraceMiddleware = None

try:
    import redis
except Exception:
    redis = None

try:
    import psycopg2
    from psycopg2.extras import RealDictCursor
    PGPY_AVAILABLE = True
except Exception:
    psycopg2 = None
    RealDictCursor = None
    PGPY_AVAILABLE = False

try:
    import google.generativeai as genai
    GENAI_AVAILABLE = True
except Exception:
    genai = None
    GENAI_AVAILABLE = False

from apscheduler.schedulers.background import BackgroundScheduler

APP_NAME = "dummy-checkout-service"

logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(message)s")
logger = logging.getLogger(APP_NAME)

app = FastAPI(title="Dummy Checkout Service")

if TraceMiddleware is not None:
    try:
        # Some ddtrace versions provide an ASGI TraceMiddleware with different
        # constructor signatures. ddtrace's `patch_all()` (used above) usually
        # instruments FastAPI/Starlette automatically, so skip adding the
        # middleware to avoid runtime TypeError from incompatible kwargs.
        logger.info("ddtrace TraceMiddleware detected but skipping explicit add_middleware to avoid compatibility issues")
    except Exception:
        pass

# Global state to simulate incidents
STATE = {"mode": "normal"}  # normal | slow | error

# DB & cache settings
DB_PATH = os.environ.get("DUMMY_DB", "./dummy.db")
REDIS_HOST = os.environ.get("REDIS_HOST", "redis")
REDIS_PORT = int(os.environ.get("REDIS_PORT", 6379))

# Redis client (optional)
_redis = None
if redis is not None:
    try:
        _redis = redis.Redis(host=REDIS_HOST, port=REDIS_PORT, decode_responses=True)
        _redis.ping()
        logger.info("Connected to Redis at %s:%s", REDIS_HOST, REDIS_PORT)
    except Exception as e:
        logger.warning("Redis not available: %s", e)


def get_db_conn():
    conn = sqlite3.connect(DB_PATH, timeout=5)
    conn.row_factory = sqlite3.Row
    return conn


def _redis_tags(key: str = None, command: str = None):
    tags = []
    try:
        tags.append(f"redis_host:{REDIS_HOST}")
        tags.append(f"redis_port:{REDIS_PORT}")
        # mark these metrics as belonging to the redis service for grouping in Datadog
        tags.append("service:redis")
        # try to infer DB number from connection pool if available
        if _redis is not None:
            try:
                dbnum = getattr(_redis, 'connection_pool').connection_kwargs.get('db', None)
                if dbnum is not None:
                    tags.append(f"redis_db:{dbnum}")
            except Exception:
                pass
        # Do NOT emit the raw Redis key as a tag to avoid high cardinality
        # in the metrics. Command and DB are sufficient for troubleshooting.
        if command:
            tags.append(f"redis_command:{command}")
    except Exception:
        pass
    return tags


# Configure tracer to point at the agent container if available
try:
    dd_agent = os.environ.get("DD_AGENT_HOST", os.environ.get("DD_AGENT", "datadog-agent"))
    if tracer is not None:
        try:
            tracer.configure(hostname=dd_agent)
            logger.info("ddtrace tracer configured to send to %s", dd_agent)
        except Exception:
            logger.debug("ddtrace tracer configure failed")
    # Attempt safe auto-instrumentation. If `patch_all()` fails we keep
    # the manual spans as a fallback so the app remains functional.
    if patch_all is not None:
        try:
            patch_all()
            logger.info("ddtrace.patch_all() applied (auto-instrumentation enabled)")
        except Exception as e:
            logger.warning("ddtrace.patch_all() failed, staying with manual spans: %s", e)
    # Configure DogStatsD to point at the agent container so statsd packets
    # are delivered (prevents "Connection refused" warnings when statsd
    # defaults to localhost inside the container).
    try:
        initialize(statsd_host=dd_agent, statsd_port=8125)
        logger.info("DogStatsD configured to send to %s:8125", dd_agent)
    except Exception:
        logger.debug("DogStatsD initialize failed")
except Exception:
    pass


UDS_PATH = "/var/run/datadog/dsd.socket"


def _send_packet(msg: str) -> bool:
    # Try Unix Domain Socket first (shared via docker volume), then fallback to UDP
    try:
        if _os.path.exists(UDS_PATH):
            try:
                s = socket.socket(socket.AF_UNIX, socket.SOCK_DGRAM)
                s.connect(UDS_PATH)
                s.send(msg.encode())
                s.close()
                return True
            except Exception:
                pass
    except Exception:
        pass

    # Fallback to UDP to DD_AGENT_HOST
    try:
        dd_agent = os.environ.get("DD_AGENT_HOST", os.environ.get("DD_AGENT", "datadog-agent"))
        s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        s.sendto(msg.encode(), (dd_agent, 8125))
        s.close()
        return True
    except Exception as e:
        logger.warning("Error submitting packet: %s", e)
        return False


def record_metric(name, value=1, tags=None):
    # Heuristic for metric type
    key = name.lower()
    if "duration" in key or key.endswith("_ms") or "latency" in key:
        metric_type = "ms"
    else:
        metric_type = "c"

    tags = _normalize_tags(tags)
    tags_str = ""
    if tags:
        tags_str = "|#" + ",".join(tags)

    packet = f"{name}:{value}|{metric_type}{tags_str}"
    _send_packet(packet)


def record_gauge(name, value, tags=None):
    tags = _normalize_tags(tags)
    packet = f"{name}:{value}|g"
    if tags:
        packet += "|#" + ",".join(tags)
    _send_packet(packet)


def record_histogram(name, value, tags=None):
    tags = _normalize_tags(tags)
    packet = f"{name}:{value}|h"
    if tags:
        packet += "|#" + ",".join(tags)
    _send_packet(packet)


def service_check(name, status=0, tags=None, message=None):
    # status: 0=OK,1=WARNING,2=CRITICAL,3=UNKNOWN
    tags = _normalize_tags(tags)
    packet = f"_sc|{name}|{status}"
    if tags:
        packet += "|#" + ",".join(tags)
    if message:
        packet += f"|m:{message}"
    _send_packet(packet)


def _normalize_tags(tags):
    """Ensure tags is a list and always include `env:<DD_ENV>`.
    Accepts None, comma-joined string, or list. Returns list.
    """
    env = os.environ.get("DD_ENV", "demo")
    out = []
    try:
        if tags is None:
            out = []
        elif isinstance(tags, str):
            out = [t for t in (tags or "").split(',') if t]
        else:
            out = list(tags)
        # strip whitespace
        out = [t.strip() for t in out if t and t.strip()]
        # ensure env tag present
        if not any(t.startswith('env:') for t in out):
            out.append(f'env:{env}')
    except Exception:
        out = [f'env:{env}']
    return out


class ChaosRequest(BaseModel):
    mode: str  # normal | slow | error


@app.on_event("startup")
def startup():
    # warm up DB
    try:
        conn = get_db_conn()
        conn.execute(
            "CREATE TABLE IF NOT EXISTS items (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT, created_at TEXT)"
        )
        conn.commit()
        conn.close()
        logger.info("DB initialized at %s", DB_PATH)
    except Exception as e:
        logger.exception("Failed to init DB: %s", e)

    # start scheduler for synthetic traffic
    scheduler = BackgroundScheduler()
    scheduler.add_job(simulate_traffic, "interval", seconds=int(os.environ.get("SIM_INTERVAL", 60)))
    # Always schedule the redis poller. The poller itself will emit a
    # CRITICAL service_check if the Redis client is unavailable so the
    # Datadog agent and UI see the failure even when Redis wasn't ready
    # at app startup.
    scheduler.add_job(poll_redis_info, "interval", seconds=int(os.environ.get("REDIS_POLL_INTERVAL", 30)))
    # Schedule a Redis workload generator so the Redis server experiences
    # more realistic traffic (sets, gets, lists, hashes, counters, pub/sub).
    scheduler.add_job(simulate_redis_traffic, "interval", seconds=int(os.environ.get("REDIS_SIM_INTERVAL", 60)))
    # Schedule Postgres poller if psycopg2 is available
    try:
        scheduler.add_job(poll_postgres_stats, "interval", seconds=int(os.environ.get("PG_POLL_INTERVAL", 30)))
    except Exception:
        pass
    # Schedule a low-frequency Postgres activity runner (default every 4 hours)
    try:
        scheduler.add_job(simulate_postgres_activity, "interval", seconds=int(os.environ.get("PG_SIM_INTERVAL_SECONDS", 86400)))
    except Exception:
        pass
    # Schedule a low-frequency Gemini Flash caller to save costs (configurable)
    # Runs at most a few times per hour by default (every 1200s = 20min)
    try:
        scheduler.add_job(simulate_gemini_query, "interval", seconds=int(os.environ.get("GEMINI_SIM_INTERVAL_SECONDS", 3600)))
    except Exception:
        # If the function isn't defined yet (older versions), ignore silently
        pass
    # Schedule occasional Redis memory spike for monitor testing (default every 12h)
    try:
        # Only schedule heavy Redis memory spikes when explicitly enabled
        if os.environ.get("REDIS_MEM_SPIKE_ENABLED", "false").lower() in ("1", "true", "yes"):
            scheduler.add_job(simulate_redis_mem_spike, "interval", seconds=int(os.environ.get("REDIS_MEM_SPIKE_INTERVAL_SECONDS", 12 * 60 * 60)))
    except Exception:
        pass
    # Schedule occasional simulated Gemini error for monitor testing (default every 12h)
    try:
        # Only schedule Gemini error injections when enabled
        if os.environ.get("GEMINI_ERROR_ENABLED", "false").lower() in ("1", "true", "yes"):
            scheduler.add_job(simulate_gemini_error, "interval", seconds=int(os.environ.get("GEMINI_ERROR_SIM_INTERVAL_SECONDS", 12 * 60 * 60)))
    except Exception:
        pass
    # Automated host stress: run at most a couple times a day by default (every 12 hours)
    try:
        def _call_host_stress_from_env():
            try:
                if os.environ.get('HOST_STRESS_ENABLED', 'true').lower() not in ('1', 'true', 'yes'):
                    return
                cpu_workers = int(os.environ.get('HOST_STRESS_CPU_WORKERS', '2'))
                cpu_duration = int(os.environ.get('HOST_STRESS_CPU_DURATION', '60'))
                mem_mb = int(os.environ.get('HOST_STRESS_MEM_MB', '200'))
                mem_hold_s = int(os.environ.get('HOST_STRESS_MEM_HOLD_S', '60'))
                for _ in range(max(1, cpu_workers)):
                    _start_cpu_burn(duration_s=cpu_duration, intensity=0.95)
                _start_mem_alloc(size_mb=mem_mb, hold_s=mem_hold_s)
                try:
                    record_metric('dummy.host_stress.trigger', 1, tags=[f'cpu_workers:{cpu_workers}', f'mem_mb:{mem_mb}'])
                except Exception:
                    pass
                logger.warning('Automated host stress triggered cpu_workers=%s cpu_duration=%s mem_mb=%s mem_hold_s=%s', cpu_workers, cpu_duration, mem_mb, mem_hold_s)
            except Exception:
                logger.exception('call_host_stress_from_env failed')

        scheduler.add_job(_call_host_stress_from_env, 'interval', hours=int(os.environ.get('HOST_STRESS_INTERVAL_HOURS', 12)))
    except Exception:
        logger.debug('Failed to schedule automated host stress')
    scheduler.start()
    logger.info("Background scheduler started (interval=%ss) and redis poller=%ss", os.environ.get("SIM_INTERVAL", 15), os.environ.get("REDIS_POLL_INTERVAL", 30))


def simulate_redis_traffic(iterations: int = 8):
    """Run a small set of Redis operations to simulate normal app traffic.

    This touches multiple Redis data types and emits metrics so the agent
    picks up activity beyond INFO polling.
    """
    global _redis
    if not _redis:
        return
    try:
        for i in range(int(os.environ.get("REDIS_SIM_BATCH", iterations))):
            # key/value (cache-like)
            key = f"dummy:item:{random.randint(1,200)}"
            try:
                _redis.set(key, str(uuid.uuid4()), ex=60)
                record_metric("dummy.redis.set", 1, tags=_redis_tags(command="SET"))
            except Exception:
                record_metric("dummy.redis.set.failed", 1, tags=_redis_tags(command="SET"))

            # get
            try:
                _redis.get(key)
                record_metric("dummy.redis.get", 1, tags=_redis_tags(command="GET"))
            except Exception:
                record_metric("dummy.redis.get.failed", 1, tags=_redis_tags(command="GET"))

            # list ops
            try:
                list_key = "dummy:queue"
                _redis.lpush(list_key, str(uuid.uuid4()))
                # occasionally pop
                if random.random() < 0.3:
                    _redis.rpop(list_key)
                record_metric("dummy.redis.list.op", 1, tags=_redis_tags(command="LPUSH/RPOP"))
            except Exception:
                record_metric("dummy.redis.list.failed", 1, tags=_redis_tags(command="LPUSH/RPOP"))

            # hash ops
            try:
                hkey = "dummy:hash"
                _redis.hset(hkey, mapping={str(random.randint(1,1000)): "v" + str(random.randint(1,1000))})
                record_metric("dummy.redis.hash.op", 1, tags=_redis_tags(command="HSET"))
            except Exception:
                record_metric("dummy.redis.hash.failed", 1, tags=_redis_tags(command="HSET"))

            # counters
            try:
                _redis.incr("dummy:counter")
                record_metric("dummy.redis.incr", 1, tags=_redis_tags(command="INCR"))
            except Exception:
                record_metric("dummy.redis.incr.failed", 1, tags=_redis_tags(command="INCR"))

            # sets and sorted sets
            try:
                _redis.sadd("dummy:uniq", str(random.randint(1,10000)))
                _redis.zadd("dummy:leader", {str(uuid.uuid4()): random.random()})
                record_metric("dummy.redis.sets", 1, tags=_redis_tags(command="SADD/ZADD"))
            except Exception:
                record_metric("dummy.redis.sets.failed", 1, tags=_redis_tags(command="SADD/ZADD"))

            # publish a short message (non-blocking)
            try:
                _redis.publish("dummy:pubsub", "ping")
                record_metric("dummy.redis.pub", 1, tags=_redis_tags(command="PUBLISH"))
            except Exception:
                record_metric("dummy.redis.pub.failed", 1, tags=_redis_tags(command="PUBLISH"))

            # small sleep between ops to spread load
            time.sleep(0.01)
    except Exception:
        logger.exception("simulate_redis_traffic failed")


def simulate_redis_mem_spike(size_kb: int = 256, count: int = 2):
    """Write large values into Redis to temporarily inflate memory usage.

    - `size_kb`: approximate size of each value in KB
    - `count`: number of keys to write
    """
    global _redis
    if not _redis:
        return
    try:
        chunk = "x" * (int(size_kb) * 1024)
        for i in range(int(count)):
            key = f"dummy:memspike:{int(time.time())}:{i}"
            try:
                _redis.set(key, chunk, ex=300)
                record_metric("dummy.redis.memspike.key_written", 1, tags=_redis_tags(command="SET"))
            except Exception:
                record_metric("dummy.redis.memspike.write_failed", 1, tags=_redis_tags(command="SET"))
        logger.info("Injected %s KB x %s keys into Redis for mem spike", size_kb, count)
    except Exception:
        logger.exception("simulate_redis_mem_spike failed")


def get_pg_conn():
    if not PGPY_AVAILABLE:
        raise RuntimeError("psycopg2 not available")
    host = os.environ.get('POSTGRES_HOST', 'postgres')
    port = int(os.environ.get('POSTGRES_PORT', 5432))
    user = os.environ.get('POSTGRES_USER', 'dummy')
    pwd = os.environ.get('POSTGRES_PASSWORD', 'dummypw')
    db = os.environ.get('POSTGRES_DB', 'dummydb')
    return psycopg2.connect(host=host, port=port, user=user, password=pwd, dbname=db, cursor_factory=RealDictCursor)


def poll_postgres_stats():
    """Poll basic Postgres stats from pg_stat_database and emit gauges/service_check."""
    if not PGPY_AVAILABLE:
        record_metric('dummy.postgres.disabled', 1)
        return
    try:
        conn = get_pg_conn()
        cur = conn.cursor()
        # Query a few helpful aggregated stats for the current DB
        cur.execute("SELECT datname, numbackends, xact_commit, xact_rollback, blks_read, blks_hit FROM pg_stat_database WHERE datname = current_database();")
        row = cur.fetchone()
        if row:
            tags = [f"service:postgres", f"db:{row.get('datname')}", f"env:{os.environ.get('DD_ENV','demo')}"]
            try:
                record_gauge('pg.numbackends', int(row.get('numbackends') or 0), tags=tags)
                record_gauge('pg.xact_commit', int(row.get('xact_commit') or 0), tags=tags)
                # Optionally emit zero rollback samples to make monitors evaluate as OK
                if os.environ.get('PG_EMIT_ZERO_ROLLBACKS', 'true').lower() in ('1', 'true', 'yes'):
                    record_gauge('pg.xact_rollback', 0, tags=tags)
                else:
                    record_gauge('pg.xact_rollback', int(row.get('xact_rollback') or 0), tags=tags)
                record_gauge('pg.blks_read', int(row.get('blks_read') or 0), tags=tags)
                record_gauge('pg.blks_hit', int(row.get('blks_hit') or 0), tags=tags)
            except Exception:
                pass
        cur.close()
        conn.close()
        # report connectivity as a service_check OK
        try:
            service_check('postgres.can_connect', status=0, tags=[f"service:postgres", f"env:{os.environ.get('DD_ENV','demo')}"], message='OK')
        except Exception:
            pass
    except Exception as e:
        logger.warning('Postgres poll failed: %s', e)
        try:
            service_check('postgres.can_connect', status=2, tags=[f"service:postgres", f"env:{os.environ.get('DD_ENV','demo')}"], message=str(e))
        except Exception:
            pass


def emit_zero_pg_rollback_loop():
    """Background thread: emit a zero-valued pg.xact_rollback gauge periodically.

    This helps demo monitors see explicit zero samples so sum-based rollback
    monitors can recover faster even if historical non-zero points exist.
    """
    interval = int(os.environ.get('PG_EMIT_ZERO_INTERVAL_SECONDS', 30))
    tags = [f"service:postgres", f"env:{os.environ.get('DD_ENV','demo')}"]
    while True:
        try:
            record_gauge('pg.xact_rollback', 0, tags=tags)
        except Exception:
            pass
        time.sleep(max(1, interval))


def emit_zero_log_errors_loop():
    """Background thread: emit a synthetic metric that represents zero log errors.

    Datadog log-based monitors count log events; you can't emit a "zero log"
    event. Instead we emit a metric `pg.log_errors` with value 0 so we can
    optionally switch monitors to use the metric (or at least have a
    synthetic gauge that shows a zero baseline in Metrics Explorer).
    """
    interval = int(os.environ.get('PG_EMIT_ZERO_LOG_ERRORS_INTERVAL_SECONDS', 30))
    tags = [f"service:postgres", f"env:{os.environ.get('DD_ENV','demo')}", f"host:{socket.gethostname()}"]
    while True:
        try:
            record_gauge('pg.log_errors', 0, tags=tags)
        except Exception:
            pass
        time.sleep(max(1, interval))


def simulate_postgres_activity():
    """Run a few lightweight Postgres operations to generate metrics and exercise the DB.

    This function inserts a row occasionally and will emit failure metrics if inserts fail.
    """
    if not PGPY_AVAILABLE:
        record_metric('dummy.postgres.disabled', 1)
        return
    try:
        batch = int(os.environ.get('PG_SIM_BATCH', 3))
        for i in range(batch):
            op = random.choice(['insert', 'select', 'update', 'delete', 'error'])
            try:
                conn = get_pg_conn()
                cur = conn.cursor()
                # ensure table exists
                cur.execute('CREATE TABLE IF NOT EXISTS items (id SERIAL PRIMARY KEY, name TEXT UNIQUE, created_at TIMESTAMP DEFAULT now())')

                if op == 'insert':
                    name = f'sim-{int(time.time())}-{random.randint(1,100000)}'
                    cur.execute('INSERT INTO items (name) VALUES (%s)', (name,))
                    conn.commit()
                    record_metric('dummy.pg.insert', 1, tags=[f'service:postgres'])

                elif op == 'select':
                    cur.execute('SELECT id, name FROM items ORDER BY RANDOM() LIMIT 1')
                    _ = cur.fetchone()
                    record_metric('dummy.pg.select', 1, tags=[f'service:postgres'])

                elif op == 'update':
                    # attempt to update a random row if exists
                    cur.execute('SELECT id FROM items ORDER BY RANDOM() LIMIT 1')
                    row = cur.fetchone()
                    if row:
                        cur.execute('UPDATE items SET name = %s WHERE id = %s', (f'upd-{int(time.time())}', row[0]))
                        conn.commit()
                        record_metric('dummy.pg.update', 1, tags=[f'service:postgres'])
                    else:
                        record_metric('dummy.pg.update.noop', 1, tags=[f'service:postgres'])

                elif op == 'delete':
                    cur.execute('SELECT id FROM items ORDER BY RANDOM() LIMIT 1')
                    row = cur.fetchone()
                    if row:
                        cur.execute('DELETE FROM items WHERE id = %s', (row[0],))
                        conn.commit()
                        record_metric('dummy.pg.delete', 1, tags=[f'service:postgres'])
                    else:
                        record_metric('dummy.pg.delete.noop', 1, tags=[f'service:postgres'])

                elif op == 'error':
                    # deliberately run a failing SQL to produce a Postgres ERROR log
                    try:
                        cur.execute('SELECT 1/0')
                        conn.commit()
                    except Exception:
                        # let exception propagate to outer handler for metrics
                        raise

                cur.close()
                conn.close()
            except Exception:
                try:
                    record_metric('dummy.pg.op.failed', 1, tags=[f'service:postgres'])
                except Exception:
                    pass
            # small pause between operations to spread load
            time.sleep(float(os.environ.get('PG_SIM_OP_DELAY_SECONDS', 0.2)))
    except Exception:
        logger.exception('simulate_postgres_activity failed')


@app.post('/pg/insert')
def pg_insert(name: str):
    """Insert a row into a Postgres table (creates table if necessary)."""
    if not PGPY_AVAILABLE:
        raise HTTPException(status_code=500, detail='psycopg2 not available in container')
    try:
        conn = get_pg_conn()
        cur = conn.cursor()
        cur.execute('CREATE TABLE IF NOT EXISTS items (id SERIAL PRIMARY KEY, name TEXT, created_at TIMESTAMP DEFAULT now())')
        cur.execute('INSERT INTO items (name) VALUES (%s) RETURNING id', (name,))
        nid = cur.fetchone().get('id')
        conn.commit()
        cur.close()
        conn.close()
        try:
            record_metric('dummy.pg.insert', 1, tags=[f'service:postgres'])
        except Exception:
            pass
        return {'inserted': True, 'id': nid}
    except Exception as e:
        logger.exception('pg_insert failed: %s', e)
        try:
            record_metric('dummy.pg.insert.failed', 1, tags=[f'service:postgres'])
            service_check('postgres.can_connect', status=2, tags=[f'service:postgres'], message=str(e))
        except Exception:
            pass
        raise HTTPException(status_code=500, detail=str(e))


@app.get('/pg/query')
def pg_query(limit: int = 10):
    """Return recent rows from items table."""
    if not PGPY_AVAILABLE:
        raise HTTPException(status_code=500, detail='psycopg2 not available')
    try:
        conn = get_pg_conn()
        cur = conn.cursor()
        cur.execute('SELECT id, name, created_at FROM items ORDER BY id DESC LIMIT %s', (int(limit),))
        rows = cur.fetchall()
        cur.close()
        conn.close()
        return {'rows': rows}
    except Exception as e:
        logger.exception('pg_query failed: %s', e)
        raise HTTPException(status_code=500, detail=str(e))


@app.post('/pg/alert')
def pg_alert():
    """Manually emit a Postgres connectivity service_check CRITICAL and optionally auto-resolve."""
    try:
        env_tag = f"env:{os.environ.get('DD_ENV','demo')}"
        service_check('postgres.can_connect', status=2, tags=[f'service:postgres', env_tag], message='Manual trigger')
        # auto-resolve after 30s
        def _resolve():
            time.sleep(30)
            try:
                env_tag = f"env:{os.environ.get('DD_ENV','demo')}"
                service_check('postgres.can_connect', status=0, tags=[f'service:postgres', env_tag], message='Auto-resolve')
            except Exception:
                pass

        threading.Thread(target=_resolve, daemon=True).start()
        return {'postgres_alert': 'sent'}
    except Exception as e:
        logger.exception('pg_alert failed')
        raise HTTPException(status_code=500, detail=str(e))


@app.post('/pg/error')
def pg_error():
    """Trigger a server-side Postgres error using the configured DB credentials.

    This executes `SELECT 1/0` via `psycopg2` so the database server logs an ERROR
    entry. Uses `get_pg_conn()` (honors `POSTGRES_USER`/`POSTGRES_DB`) instead of
    relying on an external `psql` invocation.
    """
    if not PGPY_AVAILABLE:
        raise HTTPException(status_code=500, detail='psycopg2 not available')
    try:
        conn = get_pg_conn()
        cur = conn.cursor()
        try:
            cur.execute('SELECT 1/0')
            conn.commit()
        except Exception as e:
            # Record a metric and re-raise so the error is visible in server logs
            try:
                record_metric('dummy.pg.error.triggered', 1, tags=[f'service:postgres'])
            except Exception:
                pass
            cur.close()
            conn.close()
            logger.exception('Intentional Postgres error triggered')
            raise HTTPException(status_code=500, detail=str(e))
        cur.close()
        conn.close()
        return {'error_triggered': False}
    except HTTPException:
        raise
    except Exception as e:
        logger.exception('pg_error failed: %s', e)
        raise HTTPException(status_code=500, detail=str(e))


@app.post('/redis/hit')
def redis_hit(count: int = 20):
    """Trigger a one-off burst of Redis operations from the web API."""
    threading.Thread(target=simulate_redis_traffic, args=(count,), daemon=True).start()
    logger.info("Triggered redis workload burst count=%s", count)
    return {"redis_burst": "started", "count": count}


@app.post('/redis/alert')
def redis_alert(status: int = 2, auto_resolve_after: int = 30):
    """Force a Redis service_check to a given status.

    - `status`: 0=OK,1=WARNING,2=CRITICAL,3=UNKNOWN
    - `auto_resolve_after`: seconds to wait before emitting an OK to resolve the alert (0 to skip)
    This helps test Datadog monitors that watch for `redis.can_connect`.
    """
    try:
        sc_tags = _redis_tags()
        service_check("redis.can_connect", status=int(status), tags=sc_tags, message=f"Manual trigger status={status}")
        logger.warning("Emitted manual redis.can_connect service_check status=%s tags=%s", status, sc_tags)

        if int(auto_resolve_after) > 0:
            def _resolve():
                time.sleep(int(auto_resolve_after))
                try:
                    service_check("redis.can_connect", status=0, tags=sc_tags, message="Manual resolve")
                    logger.info("Auto-resolved redis.can_connect to OK after %ss", auto_resolve_after)
                except Exception:
                    logger.exception("Failed to auto-resolve redis.can_connect")

            threading.Thread(target=_resolve, daemon=True).start()

        return {"redis_alert": "sent", "status": status, "auto_resolve_after": auto_resolve_after}
    except Exception:
        logger.exception("Failed to emit manual redis service_check")
        raise HTTPException(status_code=500, detail="Failed to emit redis alert")


@app.post('/redis/mem_spike')
def redis_mem_spike(size_kb: int = 512, count: int = 8):
    """API to trigger a Redis memory spike (writes big keys)."""
    threading.Thread(target=simulate_redis_mem_spike, args=(size_kb, count), daemon=True).start()
    return {"mem_spike": "started", "size_kb": size_kb, "count": count}


@app.get("/health")
def health():
    status = {"status": "ok", "mode": STATE["mode"]}
    logger.debug("Health check: %s", status)
    record_metric("dummy.health.check", 1, tags=[f"mode:{STATE['mode']}"])
    return status


@app.get("/work")
def work(request: Request):
    request_id = str(uuid.uuid4())
    start = time.time()
    logger.info("/work start %s mode=%s", request_id, STATE["mode"])

    # Manual tracing span (safe across ddtrace versions)
    span_ctx = tracer.trace("dummy.request") if tracer is not None else None
    if span_ctx is not None:
        try:
            span_ctx.set_tag("service", APP_NAME)
            span_ctx.set_tag("path", "/work")
        except Exception:
            pass

    # simulate latency and errors
    if STATE["mode"] == "slow":
        sleep_t = random.uniform(1.5, 3.0)
        time.sleep(sleep_t)
        record_metric("dummy.request.latency", int(sleep_t * 1000), tags=["path:work"])

    if STATE["mode"] == "error" and random.random() < 0.7:
        record_metric("dummy.request.error", 1, tags=["path:work"])
        logger.error("/work simulated error %s", request_id)
        raise HTTPException(status_code=500, detail="Simulated failure")

    # DB read
    try:
        conn = get_db_conn()
        cur = conn.execute("SELECT id, name FROM items ORDER BY RANDOM() LIMIT 1")
        row = cur.fetchone()
        conn.close()
        db_item = dict(row) if row is not None else None
        record_metric("dummy.db.read", 1)
    except Exception:
        db_item = None
        record_metric("dummy.db.read.failed", 1)
        logger.exception("DB read failed")

    # Cache hit/miss simulation
    cache_hit = False
    try:
        if _redis:
                key = f"item:{db_item['id'] if db_item else 'none'}"
                cache = _redis.get(key)
                if cache:
                    cache_hit = True
                    record_metric("dummy.cache.hit", 1, tags=_redis_tags(key=key, command="get"))
                else:
                    if db_item:
                        _redis.set(key, db_item["name"], ex=30)
                        record_metric("dummy.cache.set", 1, tags=_redis_tags(key=key, command="set"))
                    record_metric("dummy.cache.miss", 1, tags=_redis_tags(key=key, command="get"))
    except Exception:
        logger.debug("Cache operation failed")

    duration = time.time() - start
    record_metric("dummy.request.duration_ms", int(duration * 1000), tags=["path:work"])
    logger.info("/work end %s duration=%.3fs cache_hit=%s", request_id, duration, cache_hit)

    if span_ctx is not None:
        try:
            span_ctx.set_tag("duration_ms", int(duration * 1000))
            span_ctx.finish()
        except Exception:
            pass

    return {"result": "success", "mode": STATE["mode"], "item": db_item, "cache_hit": cache_hit}


@app.post("/chaos")
def chaos(req: ChaosRequest):
    if req.mode not in ["normal", "slow", "error"]:
        raise HTTPException(status_code=400, detail="Invalid mode")

    STATE["mode"] = req.mode
    logger.warning("Chaos mode changed to %s", STATE["mode"])
    record_metric("dummy.mode.change", 1, tags=[f"mode:{STATE['mode']}"])


# --- Gemini Flash caller (low frequency to save costs) ---------------------
# Support either variable name; prefer the DUMMY var if set
GEMINI_API_KEY = os.environ.get("GEMINI_DUMMY_API_KEY") or os.environ.get("GEMINI_API_KEY")


def simulate_gemini_query():
    """Perform a lightweight Gemini Flash API call using the SDK only.

    The app will only use `google.generativeai` if installed. If the
    SDK is not available, the function becomes a no-op (but emits a
    disabled metric) to avoid accidental HTTP-based calls or token leaks.
    """
    if not GENAI_AVAILABLE:
        record_metric("dummy.gemini.disabled", 1)
        logger.debug("google.generativeai SDK not available; Gemini caller disabled")
        return

    # Minimal payload; override via GEMINI_PAYLOAD env if you want something else
    payload = os.environ.get("GEMINI_PAYLOAD_JSON")
    if payload:
        try:
            json_payload = requests.compat.json.loads(payload)
        except Exception:
            json_payload = {"prompt": "hello", "max_output_tokens": 16}
    else:
        json_payload = {"prompt": "hello", "max_output_tokens": 16}

    start = time.time()
    try:
        # Try configuring the SDK directly with an API key if provided
        if GEMINI_API_KEY:
            try:
                genai.configure(api_key=GEMINI_API_KEY)
            except Exception:
                # Some genai versions/platforms prefer env-based keys; set env fallbacks
                os.environ.setdefault("GOOGLE_API_KEY", GEMINI_API_KEY)
                os.environ.setdefault("GENAI_API_KEY", GEMINI_API_KEY)
                try:
                    genai.configure(api_key=GEMINI_API_KEY)
                except Exception:
                    pass

        # As an additional fallback, try constructing a client if provided by the SDK
        client = None
        try:
            if hasattr(genai, "Client"):
                try:
                    client = genai.Client(api_key=GEMINI_API_KEY) if GEMINI_API_KEY else genai.Client()
                except Exception:
                    client = None
        except Exception:
            client = None

        model_name = os.environ.get("GEMINI_MODEL", "gemini-2.5-flash")
        prompt = json_payload.get("prompt") if isinstance(json_payload, dict) else str(json_payload)

        if client is not None:
            # try the client-based generation if available
            try:
                resp = client.generate(model=model_name, prompt=prompt)
            except Exception as e:
                raise
        else:
            # classic direct model call
            model = genai.GenerativeModel(model_name)
            resp = model.generate_content(prompt)

        duration_ms = int((time.time() - start) * 1000)
        record_metric("dummy.gemini.call", 1)
        # Record duration as a gauge so averages are meaningful for latency monitors
        record_gauge("dummy.gemini.duration_ms", duration_ms)
        text = getattr(resp, "text", None) or getattr(resp, "content", None) or str(resp)
        logger.info("Gemini (genai) call OK model=%s duration=%dms body=%s", model_name, duration_ms, str(text)[:200])
    except Exception as e:
        # Detect common ADC/auth error and give a concise troubleshooting hint
        record_metric("dummy.gemini.error", 1)
        msg = str(e)
        if "Application Default Credentials" in msg or "credentials" in msg.lower():
            logger.error("Gemini (genai) auth failed: ensure API key or ADC are available inside the container. %s", msg)
        else:
            logger.exception("Gemini (genai) call failed: %s", e)


def simulate_gemini_error():
    """Simulate a Gemini error by recording an error metric and logging.

    This does not call the real API; it helps trigger monitors that watch
    for `dummy.gemini.error`.
    """
    try:
        record_metric("dummy.gemini.error", 1)
        logger.warning("Simulated Gemini error emitted for monitoring tests")
    except Exception:
        logger.exception("simulate_gemini_error failed")


@app.post('/gemini/hit')
def gemini_hit():
    """Trigger a one-off Gemini call from the web API."""
    threading.Thread(target=simulate_gemini_query, daemon=True).start()
    logger.info("Triggered one-off Gemini call (background)")
    return {"gemini": "triggered"}


@app.post('/gemini/error')
def gemini_error():
    """Trigger a simulated Gemini error for monitor testing."""
    threading.Thread(target=simulate_gemini_error, daemon=True).start()
    return {"gemini_error": "triggered"}

    return {"mode": STATE["mode"]}


@app.post("/db/insert")
def db_insert(name: str):
    try:
        conn = get_db_conn()
        conn.execute("INSERT INTO items (name, created_at) VALUES (?, ?)", (name, datetime.utcnow().isoformat()))
        conn.commit()
        conn.close()
        record_metric("dummy.db.insert", 1)
        return {"inserted": True, "name": name}
    except Exception as e:
        logger.exception("DB insert failed: %s", e)
        raise HTTPException(status_code=500, detail="DB insert failed")


def simulate_traffic():
    """Background job that simulates traffic, exercising DB, cache, and metrics."""
    try:
        logger.info("Simulate traffic tick: mode=%s", STATE["mode"])
        span_ctx = tracer.trace("dummy.simulate_traffic") if tracer is not None else None
        if span_ctx is not None:
            try:
                span_ctx.set_tag("service", APP_NAME)
            except Exception:
                pass
        # hit work endpoint logic directly to avoid HTTP calls
        try:
            work(Request({}))
        except Exception:
            # ignore simulated errors in scheduler
            pass

        # do an occasional insert
        if random.random() < 0.2:
            name = f"seed-{int(time.time())}"
            try:
                conn = get_db_conn()
                conn.execute("INSERT INTO items (name, created_at) VALUES (?, ?)", (name, datetime.utcnow().isoformat()))
                conn.commit()
                conn.close()
                record_metric("dummy.db.insert", 1)
                logger.info("Inserted seed row %s", name)
            except Exception:
                logger.exception("Seed insert failed")

        # emit heartbeat and system metrics (include env tag so monitors see baseline)
        env_tag = f'env:{os.environ.get("DD_ENV","demo")}'
        mode_tag = f"mode:{STATE['mode']}"
        record_metric("dummy.heartbeat", 1, tags=[mode_tag, env_tag])
        # record some gauges: memory and cpu and emit a low baseline for Gemini latency
        try:
            mem = psutil.virtual_memory().percent
            cpu = psutil.cpu_percent(interval=None)
            host_tag = f'host:{socket.gethostname()}'
            record_gauge("dummy.system.memory_percent", int(mem), tags=[mode_tag, env_tag, host_tag])
            record_gauge("dummy.system.cpu_percent", int(cpu), tags=[mode_tag, env_tag, host_tag])
            try:
                gemini_base = int(os.environ.get('GEMINI_BASELINE_MS', '100'))
                record_gauge('dummy.gemini.duration_ms', gemini_base, tags=[env_tag, 'service:gemini'])
            except Exception:
                pass
        except Exception:
            pass
        if span_ctx is not None:
            try:
                span_ctx.finish()
            except Exception:
                pass
    except Exception:
        logger.exception("simulate_traffic failed")


def poll_redis_info():
    """Poll Redis INFO and emit labeled gauges and a service_check."""
    # If the redis client was not initialized at startup (e.g. Redis
    # came up after the app), report a CRITICAL service check so the
    # agent/UI know Redis is unreachable. The actual INFO polling will
    # run once a client becomes available.
    # If we don't have a client (e.g. Redis came up after app start), try
    # to connect here so the poller can self-heal instead of requiring a
    # manual restart. If connect fails, emit a CRITICAL service check.
    global _redis
    if not _redis:
        try:
            if redis is None:
                # redis package not available
                raise RuntimeError("redis package missing")
            _redis = redis.Redis(host=REDIS_HOST, port=REDIS_PORT, decode_responses=True)
            _redis.ping()
            logger.info("Redis client connected by poller at %s:%s", REDIS_HOST, REDIS_PORT)
        except Exception as e:
            sc_tags = _redis_tags()
            try:
                service_check("redis.can_connect", status=2, tags=sc_tags, message=str(e) or "Redis client unavailable")
                logger.info("Emitted service_check redis.can_connect CRITICAL tags=%s msg=%s", sc_tags, str(e) or "client-unavailable")
            except Exception:
                logger.debug("Failed to emit redis unavailable service_check")
            return
    try:
        info = _redis.info()
        base_tags = _redis_tags()
        # basic gauges
        mapping = {
            "used_memory": "redis.used_memory_bytes",
            "connected_clients": "redis.connected_clients",
            "total_commands_processed": "redis.total_commands_processed",
            "instantaneous_ops_per_sec": "redis.instantaneous_ops_per_sec",
            "keyspace_hits": "redis.keyspace_hits",
            "keyspace_misses": "redis.keyspace_misses",
        }
        for k, metric_name in mapping.items():
            v = info.get(k)
            if v is None:
                continue
            try:
                # use gauge for absolute values
                record_gauge(metric_name, int(v), tags=base_tags)
                try:
                    logger.info("Emitted metric %s=%s tags=%s", metric_name, int(v), base_tags)
                except Exception:
                    pass
            except Exception:
                logger.debug("Failed to emit metric %s", metric_name)

        # per-db key counts (db0, db1...)
        for k, v in info.items():
            if k.startswith("db") and isinstance(v, dict):
                dbname = k
                keys = v.get("keys")
                expires = v.get("expires")
                if keys is not None:
                    tags = base_tags + [f"redis_db:{dbname}"]
                    record_gauge("redis.db.keys", int(keys), tags=tags)
                    try:
                        logger.info("Emitted metric redis.db.keys=%s tags=%s", int(keys), tags)
                    except Exception:
                        pass
                if expires is not None:
                    tags = base_tags + [f"redis_db:{dbname}"]
                    record_gauge("redis.db.expires", int(expires), tags=tags)
                    try:
                        logger.info("Emitted metric redis.db.expires=%s tags=%s", int(expires), tags)
                    except Exception:
                        pass

        # emit a service check for overall connectivity
        service_check("redis.can_connect", status=0, tags=base_tags, message="OK")
        try:
            logger.info("Emitted service_check redis.can_connect OK tags=%s", base_tags)
        except Exception:
            pass
    except Exception as e:
        logger.warning("Redis poll failed: %s", e)
        sc_tags = _redis_tags()
        service_check("redis.can_connect", status=2, tags=sc_tags, message=str(e))
        try:
            logger.info("Emitted service_check redis.can_connect CRITICAL tags=%s msg=%s", sc_tags, str(e))
        except Exception:
            pass


def http_burst(target_url, count=50, delay=0.02):
    for i in range(count):
        try:
            r = requests.get(target_url, timeout=5)
            record_metric("dummy.burst.request", 1, tags=[f"status:{r.status_code}"])
            record_histogram("dummy.burst.latency_ms", int(r.elapsed.total_seconds() * 1000))
        except Exception:
            record_metric("dummy.burst.request.failed", 1)
        time.sleep(delay)


def start_background_loader(target="http://127.0.0.1:8000/work", interval=60, burst_count=20):
    def loader():
        while True:
            try:
                http_burst(target, count=burst_count, delay=0.01)
            except Exception:
                pass
            time.sleep(interval)

    t = threading.Thread(target=loader, daemon=True)
    t.start()


# start a background loader to simulate continuous traffic
try:
    start_background_loader(interval=int(os.environ.get("LOAD_INTERVAL", 60)), burst_count=int(os.environ.get("LOAD_BURST", 20)))
    logger.info("Background HTTP loader started")
except Exception:
    logger.debug("Failed to start background loader")


# Automated chaos toggler and CPU/memory utilities
ERROR_UNTIL = 0


def _start_cpu_burn(duration_s: int, intensity: float = 0.9):
    def burn():
        end = time.time() + duration_s
        while time.time() < end:
            # busy loop doing math
            x = 0.0
            for i in range(1000):
                x += (i ** 2) ** 0.5
            # yield a tiny bit
            time.sleep(max(0.0, (1.0 - intensity) * 0.01))

    t = threading.Thread(target=burn, daemon=True)
    t.start()


def _start_mem_alloc(size_mb: int, hold_s: int):
    def alloc():
        try:
            arr = bytearray(size_mb * 1024 * 1024)
            time.sleep(hold_s)
            # free
            del arr
            gc.collect()
        except Exception:
            pass

    t = threading.Thread(target=alloc, daemon=True)
    t.start()


@app.post('/burst')
def burst_endpoint(count: int = 50, delay: float = 0.01, target: str = None):
    target_url = target or "http://127.0.0.1:8000/work"
    threading.Thread(target=http_burst, args=(target_url, int(count), float(delay)), daemon=True).start()
    logger.info("Triggered burst: count=%s delay=%s target=%s", count, delay, target_url)
    return {"burst": "started", "count": count, "delay": delay}


@app.post('/loadcpu')
def loadcpu(duration: int = 10, intensity: float = 0.9):
    threading.Thread(target=_start_cpu_burn, args=(int(duration), float(intensity)), daemon=True).start()
    logger.info("Started cpu burn for %ss intensity=%s", duration, intensity)
    return {"cpu_burn": "started", "duration": duration, "intensity": intensity}


@app.post('/memalloc')
def memalloc(size_mb: int = 100, hold_s: int = 30):
    threading.Thread(target=_start_mem_alloc, args=(int(size_mb), int(hold_s)), daemon=True).start()
    logger.info("Started memory alloc %sMB for %ss", size_mb, hold_s)
    return {"mem_alloc": "started", "size_mb": size_mb, "hold_s": hold_s}


# Generic emitters to allow manually triggering monitors/alerts
@app.post('/emit/metric')
def emit_metric(name: str, value: float = 1.0, mtype: str = 'g', tags: str = None, count: int = 1):
    """Emit a metric repeatedly. `mtype`: 'g' (gauge), 'c' (count), 'h' (histogram). `tags` is comma-separated."""
    tag_list = [t for t in (tags or '').split(',') if t]
    for i in range(int(count)):
        try:
            if mtype == 'g':
                record_gauge(name, float(value), tags=tag_list)
            elif mtype == 'h':
                record_histogram(name, float(value), tags=tag_list)
            else:
                record_metric(name, float(value), tags=tag_list)
        except Exception:
            logger.exception('Failed emitting metric %s', name)
    return {"emitted": name, "value": value, "type": mtype, "count": count}


@app.post('/emit/service_check')
def emit_service_check(name: str, status: int = 2, tags: str = None, message: str = None, auto_resolve_after: int = 0):
    """Emit a service_check and optionally auto-resolve it after seconds."""
    tag_list = [t for t in (tags or '').split(',') if t]
    try:
        service_check(name, status=int(status), tags=tag_list, message=message)
        logger.warning("Emitted service_check %s status=%s tags=%s message=%s", name, status, tag_list, message)
    except Exception:
        logger.exception("Failed to emit service_check %s", name)
        raise HTTPException(status_code=500, detail="Failed to emit service_check")

    if int(auto_resolve_after) > 0:
        def _resolve():
            time.sleep(int(auto_resolve_after))
            try:
                service_check(name, status=0, tags=tag_list, message="Auto-resolved")
                logger.info("Auto-resolved service_check %s to OK after %ss", name, auto_resolve_after)
            except Exception:
                logger.exception("Failed to auto-resolve service_check %s", name)

        threading.Thread(target=_resolve, daemon=True).start()

    return {"service_check": name, "status": status, "auto_resolve_after": auto_resolve_after}


@app.post('/trigger/gemini_errors')
def trigger_gemini_errors(count: int = 10):
    """Emit `dummy.gemini.error` metric `count` times to help fire Gemini error monitors."""
    for i in range(int(count)):
        record_metric('dummy.gemini.error', 1, tags=[f'instance:manual'])
    logger.info('Emitted %s dummy.gemini.error metrics', count)
    return {"emitted": "dummy.gemini.error", "count": count}


@app.on_event('startup')
def start_automations():
    # run a light toggler: occasionally flip into error mode to trigger service checks
    def toggler():
        global ERROR_UNTIL
        while True:
            try:
                if ERROR_UNTIL and time.time() < ERROR_UNTIL:
                    # currently in error window
                    time.sleep(5)
                    continue

                # Make chaos behaviour configurable via env for lower frequency
                chaos_prob = float(os.environ.get('AUTOMATED_CHAOS_PROBABILITY', '0.01'))
                chaos_min = int(os.environ.get('AUTOMATED_CHAOS_MIN_DURATION_SECONDS', '15'))
                chaos_max = int(os.environ.get('AUTOMATED_CHAOS_MAX_DURATION_SECONDS', '45'))
                cpu_spike_prob = float(os.environ.get('AUTOMATED_CPU_SPIKE_PROBABILITY', '0.03'))
                mem_spike_prob = float(os.environ.get('AUTOMATED_MEM_SPIKE_PROBABILITY', '0.02'))
                sleep_between_checks = int(os.environ.get('AUTOMATED_CHAOS_SLEEP_SECONDS', '30'))

                # random chance to start an error window
                if random.random() < chaos_prob:
                    # set error mode for a shorter, configurable duration
                    duration = random.randint(chaos_min, chaos_max)
                    ERROR_UNTIL = time.time() + duration
                    STATE['mode'] = 'error'
                    logger.warning('Automated chaos: entering error mode for %ss', duration)
                    service_check('dummy.service', status=2, tags=[f"mode:{STATE['mode']}"], message='Simulated CRITICAL')
                    time.sleep(duration)
                    STATE['mode'] = 'normal'
                    ERROR_UNTIL = 0
                    logger.warning('Automated chaos: restored normal mode')
                    service_check('dummy.service', status=0, tags=[f"mode:{STATE['mode']}"], message='Recovered')
                else:
                    # sometimes trigger a small cpu or mem spike, less frequently
                    if random.random() < cpu_spike_prob:
                        _start_cpu_burn(duration_s=random.randint(3, 10), intensity=0.95)
                        logger.info('Automated cpu spike started')
                    if random.random() < mem_spike_prob:
                        _start_mem_alloc(size_mb=random.randint(30, 100), hold_s=random.randint(8, 30))
                        logger.info('Automated mem spike started')
                time.sleep(sleep_between_checks)
            except Exception:
                time.sleep(5)

    # Only enable automated chaos when explicitly allowed via env
    if os.environ.get('AUTOMATED_CHAOS_ENABLED', 'false').lower() in ('1', 'true', 'yes'):
        t = threading.Thread(target=toggler, daemon=True)
        t.start()
    else:
        logger.info('Automated chaos toggler disabled via AUTOMATED_CHAOS_ENABLED')

    # Periodic, deterministic spike generator: emits a short, configurable
    # window of noisy/high-latency metrics so monitors go ALERT and then
    # automatically recover. This makes monitors OK most of the time but
    # reproducibly spike on a schedule for demo/testing.
    def periodic_spike():
        while True:
            try:
                interval_min = int(os.environ.get('SPIKE_INTERVAL_MINUTES', 30))
                # wait until next spike window
                time.sleep(max(1, interval_min) * 60)

                duration = int(os.environ.get('SPIKE_DURATION_SECONDS', 60))
                types = [t.strip() for t in os.environ.get('SPIKE_TYPES', 'gemini,postgres,host').split(',') if t.strip()]

                logger.warning('Periodic spike starting for %ss types=%s', duration, types)
                prev_mode = STATE.get('mode', 'normal')
                # Enter an error-ish mode so endpoints that respect STATE will act
                STATE['mode'] = 'error'

                # Intensified emissions: stronger, more frequent signals
                if 'gemini' in types:
                    # Emit many high-latency gauge samples so latency monitors evaluate
                    gemini_ms = int(os.environ.get('SPIKE_GEMINI_MS', 30000))
                    for _ in range(int(os.environ.get('SPIKE_GEMINI_COUNT', 2000))):
                        try:
                            record_gauge('dummy.gemini.duration_ms', gemini_ms, tags=[f'service:gemini', f'env:{os.environ.get("DD_ENV","demo")}'])
                            record_metric('dummy.gemini.error', 1, tags=[f'service:gemini'])
                        except Exception:
                            pass
                        time.sleep(0.005)
                    logger.error('Periodic spike: injected heavy Gemini failures (gauge fallback)')

                if 'postgres' in types:
                    try:
                        service_check('postgres.can_connect', status=2, tags=[f'service:postgres', f'env:{os.environ.get("DD_ENV","demo")}'], message='Periodic spike')
                    except Exception:
                        pass
                    # bulk emit failed insert samples and explicit ERROR logs (increase volume)
                    failed_count = int(os.environ.get('SPIKE_PG_FAILED_COUNT', 2000))
                    for _ in range(failed_count):
                        try:
                            record_metric('dummy.pg.insert.failed', 1, tags=[f'service:postgres', f'host:{socket.gethostname()}', f'env:{os.environ.get("DD_ENV","demo")}'])
                        except Exception:
                            pass
                        time.sleep(0.002)
                    logger.error('Periodic spike: injected Postgres failed inserts x%s', failed_count)

                    # try multiple server-side errors to generate ERROR logs
                    if PGPY_AVAILABLE:
                        for _ in range(int(os.environ.get('SPIKE_PG_SERVER_ERRORS', 12))):
                            try:
                                conn = get_pg_conn()
                                cur = conn.cursor()
                                try:
                                    cur.execute('SELECT 1/0')
                                except Exception:
                                    logger.error('Intentional Postgres server-side error (simulated)')
                                finally:
                                    try:
                                        cur.close()
                                    except Exception:
                                        pass
                                    try:
                                        conn.close()
                                    except Exception:
                                        pass
                            except Exception:
                                pass

                    # Emit additional postgres signals that common monitors use (increase rollbacks/log-errors)
                    try:
                        tags = [f'service:postgres', f'host:{socket.gethostname()}', f'env:{os.environ.get("DD_ENV","demo")}']
                        # simulate very low connections repeatedly
                        numbackends_count = int(os.environ.get('SPIKE_PG_NUMBACKENDS_COUNT', 200))
                        for _ in range(numbackends_count):
                            record_gauge('pg.numbackends', 0, tags=tags)
                            time.sleep(0.005)
                        # emit many rollback samples so rollback-based monitors detect failures
                        rollback_count = int(os.environ.get('SPIKE_PG_ROLLBACK_COUNT', 300))
                        for _ in range(rollback_count):
                            record_gauge('pg.xact_rollback', random.randint(1, 5), tags=tags)
                            time.sleep(0.005)
                        # emit repeated positive log-errors metric to help log-based monitors
                        logerr_count = int(os.environ.get('SPIKE_PG_LOG_ERRORS_COUNT', 200))
                        for _ in range(logerr_count):
                            record_gauge('pg.log_errors', random.randint(1, 5), tags=tags + [f'host:{socket.gethostname()}'])
                            time.sleep(0.005)
                    except Exception:
                        pass

                if 'host' in types:
                    try:
                        # Emit synthetic high host metrics so host-based monitors evaluate
                        host_tag = f'host:{socket.gethostname()}'
                        for _ in range(int(os.environ.get('SPIKE_HOST_GAUGE_COUNT', 60))):
                            # emit high synthetic values rather than relying solely on psutil
                            record_metric('dummy.host.cpu.percent', 95, tags=['service:host'])
                            record_metric('dummy.host.mem.percent', 85, tags=['service:host'])
                            # emit the specific gauges used by our monitors (env-tagged)
                            env_tag = f'env:{os.environ.get("DD_ENV","demo")}'
                            try:
                                record_gauge('dummy.system.cpu_percent', 95, tags=[env_tag])
                                record_gauge('dummy.system.memory_percent', 85, tags=[env_tag])
                            except Exception:
                                pass
                            record_gauge('system.cpu.user', 90, tags=[host_tag])
                            record_gauge('system.mem.used', int(psutil.virtual_memory().used * 1.2), tags=[host_tag])
                            record_gauge('system.mem.pct', 85, tags=[host_tag])
                            time.sleep(0.3)
                    except Exception:
                        pass
                    logger.error('Periodic spike: injected host stress signals (synthetic gauges)')
                    _start_cpu_burn(duration_s=min(120, max(10, int(os.environ.get('SPIKE_HOST_CPU_SECONDS', 60)))), intensity=0.99)
                    _start_mem_alloc(size_mb=min(800, max(128, int(os.environ.get('SPIKE_HOST_MEM_MB', 400)))), hold_s=min(120, max(30, int(os.environ.get('SPIKE_HOST_MEM_HOLD_S', 60)))))

                if 'redis' in types:
                    try:
                        service_check('redis.can_connect', status=2, tags=[f'service:redis', f'env:{os.environ.get("DD_ENV","demo")}'], message='Periodic spike')
                    except Exception:
                        pass
                    redis_failed = int(os.environ.get('SPIKE_REDIS_FAILED_COUNT', 2000))
                    for _ in range(redis_failed):
                        try:
                            record_metric('dummy.redis.op.failed', 1, tags=[f'service:redis', f'host:{socket.gethostname()}', f'env:{os.environ.get("DD_ENV","demo")}'])
                        except Exception:
                            pass
                        time.sleep(0.002)
                    logger.error('Periodic spike: injected Redis failed ops x%s', redis_failed)
                    try:
                        simulate_redis_mem_spike(size_kb=int(os.environ.get('SPIKE_REDIS_MEM_KB', 2048)), count=int(os.environ.get('SPIKE_REDIS_MEM_COUNT', 32)))
                    except Exception:
                        logger.debug('Redis mem spike not applied or failed')

                # keep spike window for configured duration
                time.sleep(max(1, duration))

                # Resolve: clear state and emit OK service checks so monitors recover
                STATE['mode'] = prev_mode
                try:
                    service_check('dummy.service', status=0, tags=[f'mode:{STATE["mode"]}'], message='Periodic spike resolved')
                except Exception:
                    pass
                if 'postgres' in types:
                    try:
                        service_check('postgres.can_connect', status=0, tags=[f'service:postgres', f'env:{os.environ.get("DD_ENV","demo")}'], message='Recovered')
                    except Exception:
                        pass

                logger.warning('Periodic spike ended; returning to mode=%s', STATE['mode'])
            except Exception:
                logger.exception('periodic_spike failed, continuing')
                time.sleep(5)

    if os.environ.get('SPIKE_ENABLED', 'true').lower() in ('1', 'true', 'yes'):
        ps = threading.Thread(target=periodic_spike, daemon=True)
        ps.start()
    else:
        logger.info('Periodic spike scheduler disabled via SPIKE_ENABLED')

    # Start optional zero-rollback emitter so monitors see explicit zeros
    if os.environ.get('PG_EMIT_ZERO_ROLLBACKS', 'true').lower() in ('1', 'true', 'yes'):
        try:
            zr = threading.Thread(target=emit_zero_pg_rollback_loop, daemon=True)
            zr.start()
            logger.info('Started pg.xact_rollback zero-emitter every %ss', os.environ.get('PG_EMIT_ZERO_INTERVAL_SECONDS', 30))
        except Exception:
            logger.exception('Failed to start pg.xact_rollback zero-emitter')
    # Start optional zero-log-errors emitter
    if os.environ.get('PG_EMIT_ZERO_LOG_ERRORS', 'false').lower() in ('1', 'true', 'yes'):
        try:
            zl = threading.Thread(target=emit_zero_log_errors_loop, daemon=True)
            zl.start()
            logger.info('Started pg.log_errors zero-emitter every %ss', os.environ.get('PG_EMIT_ZERO_LOG_ERRORS_INTERVAL_SECONDS', 30))
        except Exception:
            logger.exception('Failed to start pg.log_errors zero-emitter')

@app.post('/spike/trigger')
def spike_trigger(types: str = 'gemini,postgres,host', duration: int = None):
    """Manually trigger a spike window immediately.

    - `types`: comma-separated list of components to spike (gemini, postgres, host)
    - `duration`: seconds to keep the spike window (default from `SPIKE_DURATION_SECONDS`)
    """
    def _run_spike(spec_types, dur):
        prev = STATE.get('mode', 'normal')
        STATE['mode'] = 'error'
        try:
            logger.warning('Manual spike starting duration=%s types=%s', dur, spec_types)
            # Gemin i: emit lots of high-latency/error metrics
            if 'gemini' in spec_types:
                # Emit gauge-style latency samples (not counters) so latency monitors
                # that expect `dummy.gemini.duration_ms` as a gauge see the spike.
                gemini_ms = int(os.environ.get('SPIKE_GEMINI_MS', 30000))
                for _ in range(50):
                    try:
                        record_gauge('dummy.gemini.duration_ms', gemini_ms, tags=[f'service:gemini', f'env:{os.environ.get("DD_ENV","demo")}'])
                        record_metric('dummy.gemini.error', 1, tags=[f'service:gemini'])
                    except Exception:
                        pass
                    time.sleep(0.01)
                logger.error('Manual spike: injected heavy Gemini failures')

            if 'postgres' in spec_types:
                try:
                    service_check('postgres.can_connect', status=2, tags=[f'service:postgres', f'env:{os.environ.get("DD_ENV","demo")}'], message='Manual spike')
                except Exception:
                    pass
                # emit many failure metrics quickly
                for _ in range(max(50, int(dur * 10))):
                    try:
                        record_metric('dummy.pg.insert.failed', 1, tags=[f'service:postgres', f'host:{socket.gethostname()}', f'env:{os.environ.get("DD_ENV","demo")}'])
                    except Exception:
                        pass
                    time.sleep(0.01)
                logger.error('Manual spike: injected Postgres failed inserts burst')
                # Try multiple server-side errors to generate ERROR logs
                if PGPY_AVAILABLE:
                    for _ in range(4):
                        try:
                            conn = get_pg_conn()
                            cur = conn.cursor()
                            try:
                                cur.execute('SELECT 1/0')
                            except Exception:
                                logger.error('Intentional Postgres server-side error (manual)')
                            finally:
                                try:
                                    cur.close()
                                except Exception:
                                    pass
                                try:
                                    conn.close()
                                except Exception:
                                    pass
                        except Exception:
                            # ignore connection/setup failures
                            pass

                # Also emit explicit postgres signals: low connections, rollbacks, log errors
                try:
                    pgtags = ["service:postgres", f"host:{socket.gethostname()}", f"env:{os.environ.get('DD_ENV','demo')}"]
                    record_gauge('pg.numbackends', 0, tags=pgtags)
                    for _ in range(8):
                        record_gauge('pg.xact_rollback', random.randint(1, 10), tags=pgtags)
                    record_gauge('pg.log_errors', random.randint(1, 10), tags=pgtags + [f'host:{socket.gethostname()}'])
                except Exception:
                    pass

            if 'host' in spec_types:
                try:
                    # Emit synthetic host-level counters and also the specific
                    # `dummy.system.*` gauges (with env tag) that our monitors watch.
                    env_tag = f'env:{os.environ.get("DD_ENV","demo")}'
                    emit_count = max(20, int(os.environ.get('SPIKE_HOST_GAUGE_COUNT', 20)))
                    for _ in range(emit_count):
                        record_metric('dummy.host.cpu.percent', int(psutil.cpu_percent()), tags=['service:host'])
                        record_metric('dummy.host.mem.percent', int(psutil.virtual_memory().percent), tags=['service:host'])
                        # Ensure monitors that look at `dummy.system.cpu_percent`/`memory_percent`
                        # receive explicit env-tagged gauge samples during manual spikes.
                        try:
                            record_gauge('dummy.system.cpu_percent', int(psutil.cpu_percent()), tags=[env_tag])
                            record_gauge('dummy.system.memory_percent', int(psutil.virtual_memory().percent), tags=[env_tag])
                        except Exception:
                            pass
                        time.sleep(0.01)
                except Exception:
                    pass
                logger.error('Manual spike: injected host stress signals')
                # Allow larger CPU burn and memory alloc during manual spikes; respect env vars
                cpu_dur = min(600, max(5, int(os.environ.get('SPIKE_HOST_CPU_SECONDS', 30))))
                mem_mb = min(4096, max(64, int(os.environ.get('SPIKE_HOST_MEM_MB', 200))))
                mem_hold = min(600, max(10, int(os.environ.get('SPIKE_HOST_MEM_HOLD_S', 30))))
                _start_cpu_burn(duration_s=cpu_dur, intensity=0.98)
                _start_mem_alloc(size_mb=mem_mb, hold_s=mem_hold)
                # Emit host-level gauges for monitors and repeat based on env-configured count
                try:
                    host_tag = f'host:{socket.gethostname()}'
                    for _ in range(emit_count):
                        record_gauge('system.cpu.user', int(psutil.cpu_percent()), tags=[host_tag])
                        record_gauge('system.mem.used', int(psutil.virtual_memory().used), tags=[host_tag])
                        record_gauge('system.mem.pct', int(psutil.virtual_memory().percent), tags=[host_tag])
                        time.sleep(0.01)
                except Exception:
                    pass

            if 'redis' in spec_types:
                try:
                    service_check('redis.can_connect', status=2, tags=[f'service:redis', f'env:{os.environ.get("DD_ENV","demo")}'], message='Manual spike')
                except Exception:
                    pass
                for _ in range(max(100, int(dur * 5))):
                    try:
                        record_metric('dummy.redis.op.failed', 1, tags=[f'service:redis', f'host:{socket.gethostname()}', f'env:{os.environ.get("DD_ENV","demo")}'])
                    except Exception:
                        pass
                    time.sleep(0.005)
                logger.error('Manual spike: injected Redis failed ops burst')
                try:
                    simulate_redis_mem_spike(size_kb=int(os.environ.get('SPIKE_REDIS_MEM_KB', 1024)), count=int(os.environ.get('SPIKE_REDIS_MEM_COUNT', 8)))
                except Exception:
                    logger.debug('Redis mem spike not applied or failed')

            # keep the spike for the requested duration
            time.sleep(max(1, dur))
        finally:
            STATE['mode'] = prev
            try:
                service_check('dummy.service', status=0, tags=[f'mode:{STATE["mode"]}'], message='Manual spike resolved')
            except Exception:
                pass
            try:
                service_check('postgres.can_connect', status=0, tags=[f'service:postgres', f'env:{os.environ.get("DD_ENV","demo")}'], message='Recovered')
            except Exception:
                pass
            logger.warning('Manual spike ended; returned to mode=%s', STATE['mode'])

    spec = [t.strip() for t in (types or '').split(',') if t.strip()]
    dur = int(duration) if duration is not None else int(os.environ.get('SPIKE_DURATION_SECONDS', 60))
    threading.Thread(target=_run_spike, args=(spec, dur), daemon=True).start()
    return {'spike_triggered': True, 'types': spec, 'duration': dur}

    # Gemini heartbeat: emit low-latency metrics periodically so latency monitor has data
    def emit_gemini_heartbeat():
        while True:
            try:
                # emit a small random latency so the monitor sees normal behavior
                val = random.randint(20, 80)
                record_metric('dummy.gemini.duration_ms', int(val))
                record_metric('dummy.gemini.call', 1)
            except Exception:
                logger.exception('emit_gemini_heartbeat failed')
            # sleep based on env or default 60s
            time.sleep(int(os.environ.get('GEMINI_HEARTBEAT_INTERVAL', 60)))

    if os.environ.get('GEMINI_HEARTBEAT_ENABLED', 'true').lower() in ('1', 'true', 'yes'):
        gh = threading.Thread(target=emit_gemini_heartbeat, daemon=True)
        gh.start()
    else:
        logger.info('Gemini heartbeat disabled via GEMINI_HEARTBEAT_ENABLED')

    # Baseline emitter: explicitly emit lightweight metrics regularly so
    # monitors always have datapoints outside of spike windows. This
    # prevents the appearance of "no data" or disappearing series after
    # we resolve spikes or create downtimes.
    def emit_baseline_metrics():
        interval = int(os.environ.get('BASELINE_EMIT_INTERVAL_SECONDS', 15))
        env_tag = f"env:{os.environ.get('DD_ENV','demo')}"
        host_tag = f"host:{socket.gethostname()}"
        while True:
            try:
                # Gemini baseline: emit several low-latency gauge samples so
                # latency monitors see normal behavior outside spike windows.
                try:
                    for _ in range(int(os.environ.get('GEMINI_BASELINE_COUNT', 3))):
                        val = random.randint(20, 80)
                        record_metric('dummy.gemini.call', 1, tags=[env_tag])
                        record_gauge('dummy.gemini.duration_ms', int(val), tags=[env_tag])
                        time.sleep(0.01)
                except Exception:
                    pass

                # Postgres baseline: emit explicit zero rollback gauge so
                # sum-based rollback monitors evaluate to 0 in their window.
                try:
                    pg_tags = [f'service:postgres', env_tag, host_tag]
                    record_gauge('pg.xact_rollback', 0, tags=pg_tags)
                    # Emit occasional low `pg.numbackends` samples to help
                    # low-connection monitors see small dips in normal traffic
                    if random.random() < float(os.environ.get('PG_BASELINE_LOW_CONN_PROB', '0.05')):
                        record_gauge('pg.numbackends', int(os.environ.get('PG_BASELINE_LOW_CONN_VALUE', '1')), tags=pg_tags)
                    # Emit small positive pg.log_errors occasionally so monitors
                    # that look for non-zero log errors have low baseline samples
                    if random.random() < float(os.environ.get('PG_BASELINE_LOG_ERROR_PROB', '0.02')):
                        record_gauge('pg.log_errors', 1, tags=pg_tags + [host_tag])
                except Exception:
                    pass

                # Host/system baseline: emit CPU/memory and heartbeat
                try:
                    mem = int(psutil.virtual_memory().percent)
                    cpu = int(psutil.cpu_percent(interval=None))
                    record_gauge('dummy.system.memory_percent', int(mem), tags=[env_tag])
                    record_gauge('dummy.system.cpu_percent', int(cpu), tags=[env_tag])
                    record_metric('dummy.heartbeat', 1, tags=[env_tag])
                except Exception:
                    pass

            except Exception:
                logger.exception('emit_baseline_metrics failed')
            time.sleep(max(1, interval))

    if os.environ.get('BASELINE_EMITTER_ENABLED', 'true').lower() in ('1', 'true', 'yes'):
        try:
            be = threading.Thread(target=emit_baseline_metrics, daemon=True)
            be.start()
            logger.info('Started baseline metrics emitter every %ss', os.environ.get('BASELINE_EMIT_INTERVAL_SECONDS', 15))
        except Exception:
            logger.exception('Failed to start baseline metrics emitter')


# --- New endpoints: Datadog query + bulk emit + host stress ------------------
@app.get('/dd/query')
def dd_query(q: str = 'avg:dummy.gemini.duration_ms{*}', minutes: int = 15):
    """Query Datadog Metrics API using credentials available in the container's .env.

    Returns the raw JSON response from Datadog's `/api/v1/query` endpoint.
    """
    dd_site = os.environ.get('DD_SITE')
    dd_api = os.environ.get('DD_API_KEY')
    dd_app = os.environ.get('DD_APP_KEY')
    if not dd_site or not dd_api or not dd_app:
        raise HTTPException(status_code=500, detail='Missing Datadog credentials in environment')
    to = int(time.time())
    frm = to - int(minutes) * 60
    try:
        url = f"https://{dd_site}/api/v1/query?from={frm}&to={to}&query={requests.utils.quote(q)}"
        resp = requests.get(url, headers={
            'DD-API-KEY': dd_api,
            'DD-APPLICATION-KEY': dd_app,
        }, timeout=20)
        resp.raise_for_status()
        return resp.json()
    except Exception as e:
        logger.exception('Datadog query failed')
        raise HTTPException(status_code=502, detail=str(e))


@app.post('/dd/emit_gauge_fallback')
def dd_emit_gauge_fallback(name: str = 'dummy.gemini.duration_ms', value: float = 25000.0, count: int = 1000, tags: str = 'service:gemini,env:demo'):
    """Emit `count` gauge samples for `name` quickly to force Datadog ingestion visibility.

    Runs emission in a background thread and returns immediately.
    """
    tag_list = [t for t in (tags or '').split(',') if t]
    # ensure a host tag is present so per-host monitors (by {host}) can evaluate
    try:
        has_host = any(t.startswith('host:') for t in tag_list)
    except Exception:
        has_host = False
    if not has_host:
        try:
            host_tag = f"host:{socket.gethostname()}"
            tag_list.append(host_tag)
        except Exception:
            pass

    def _emit_loop():
        for i in range(int(count)):
            try:
                record_gauge(name, float(value), tags=tag_list)
            except Exception:
                pass
            # tiny sleep to avoid CPU spin
            time.sleep(0.005)
        logger.info('Completed bulk gauge emit %s x%s', name, count)

    threading.Thread(target=_emit_loop, daemon=True).start()
    return {"emitting": name, "count": count}


@app.post('/emit/pg_failed')
def emit_pg_failed(count: int = 200, value: float = 1.0, tags: str = 'service:postgres,env:demo'):
    """Bulk emit `dummy.pg.insert.failed` samples with a host tag so monitors pick them up.

    Use this to force the Postgres failed-insert monitor to see data.
    """
    tag_list = [t for t in (tags or '').split(',') if t]
    try:
        if not any(t.startswith('host:') for t in tag_list):
            tag_list.append(f"host:{socket.gethostname()}")
    except Exception:
        pass

    def _emit_loop():
        for i in range(int(count)):
            try:
                record_metric('dummy.pg.insert.failed', float(value), tags=tag_list)
            except Exception:
                pass
            time.sleep(0.01)
        logger.info('Completed bulk emit dummy.pg.insert.failed x%s', count)

    threading.Thread(target=_emit_loop, daemon=True).start()
    return {"emitting": 'dummy.pg.insert.failed', "count": count}


@app.post('/host/stress')
def host_stress(cpu_workers: int = 2, cpu_duration: int = 60, mem_mb: int = 200, mem_hold_s: int = 60):
    """Start several concurrent CPU burners and a memory allocation to provoke host metrics.

    These stressors run inside the `dummy_app` container and have historically been sufficient
    to trigger the Datadog host monitors in this compose setup.
    """
    # Kick off multiple CPU burners
    for i in range(int(cpu_workers)):
        _start_cpu_burn(duration_s=int(cpu_duration), intensity=0.95)

    # Start a memory allocation
    _start_mem_alloc(size_mb=int(mem_mb), hold_s=int(mem_hold_s))

    logger.warning('Host stress started cpu_workers=%s cpu_duration=%s mem_mb=%s mem_hold_s=%s', cpu_workers, cpu_duration, mem_mb, mem_hold_s)
    return {"host_stress": "started", "cpu_workers": cpu_workers, "cpu_duration": cpu_duration, "mem_mb": mem_mb, "mem_hold_s": mem_hold_s}

