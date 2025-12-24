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

    tags_str = ""
    if tags:
        # tags come in as list like ['k:v']
        tags_str = "|#" + ",".join(tags)

    packet = f"{name}:{value}|{metric_type}{tags_str}"
    _send_packet(packet)


def record_gauge(name, value, tags=None):
    packet = f"{name}:{value}|g"
    if tags:
        packet += "|#" + ",".join(tags)
    _send_packet(packet)


def record_histogram(name, value, tags=None):
    packet = f"{name}:{value}|h"
    if tags:
        packet += "|#" + ",".join(tags)
    _send_packet(packet)


def service_check(name, status=0, tags=None, message=None):
    # status: 0=OK,1=WARNING,2=CRITICAL,3=UNKNOWN
    packet = f"_sc|{name}|{status}"
    if tags:
        packet += "|#" + ",".join(tags)
    if message:
        packet += f"|m:{message}"
    _send_packet(packet)


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
    scheduler.add_job(simulate_traffic, "interval", seconds=int(os.environ.get("SIM_INTERVAL", 15)))
    scheduler.start()
    logger.info("Background scheduler started (interval=%ss)", os.environ.get("SIM_INTERVAL", 15))


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
                record_metric("dummy.cache.hit", 1)
            else:
                if db_item:
                    _redis.set(key, db_item["name"], ex=30)
                record_metric("dummy.cache.miss", 1)
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

        # emit heartbeat and system metrics
        record_metric("dummy.heartbeat", 1, tags=[f"mode:{STATE['mode']}"])
        # record some gauges: memory and cpu
        try:
            mem = psutil.virtual_memory().percent
            cpu = psutil.cpu_percent(interval=None)
            record_gauge("dummy.system.memory_percent", int(mem), tags=[f"mode:{STATE['mode']}"])
            record_gauge("dummy.system.cpu_percent", int(cpu), tags=[f"mode:{STATE['mode']}"])
        except Exception:
            pass
        if span_ctx is not None:
            try:
                span_ctx.finish()
            except Exception:
                pass
    except Exception:
        logger.exception("simulate_traffic failed")


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

                # random chance to start an error window
                if random.random() < 0.06:
                    # set error mode for 30-90s
                    duration = random.randint(30, 90)
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
                    # sometimes trigger a cpu or mem spike
                    if random.random() < 0.08:
                        _start_cpu_burn(duration_s=random.randint(5, 20), intensity=0.95)
                        logger.info('Automated cpu spike started')
                    if random.random() < 0.05:
                        _start_mem_alloc(size_mb=random.randint(50, 200), hold_s=random.randint(10, 40))
                        logger.info('Automated mem spike started')
                time.sleep(10)
            except Exception:
                time.sleep(5)

    t = threading.Thread(target=toggler, daemon=True)
    t.start()

