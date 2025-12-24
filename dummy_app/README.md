# Dummy App (Datadog demo)

This tiny FastAPI app emits metrics, logs, traces, and exercises a small SQLite DB and Redis cache so a Datadog Agent can monitor it.

Quick start (Docker Compose)

1. Export your Datadog API key:

```powershell
setx DD_API_KEY "<YOUR_DATADOG_API_KEY>"
```

2. Build and start the stack:

```bash
docker-compose up --build
```

Services:
- `app`: the FastAPI dummy application (port 8000)
- `redis`: Redis for cache monitoring
- `datadog`: Datadog Agent to collect metrics/logs/traces (requires `DD_API_KEY`)

Notes:
- The app uses DogStatsD (UDP 8125) and APM (8126) to send to the Datadog Agent. The agent must be reachable from the app container (compose provides this).
- To initialize a seeded DB locally before running the app (if running without Docker):

```bash
python init_db.py
uvicorn app:app --host 0.0.0.0 --port 8000
```

Endpoints:
- `GET /health` - basic health check
- `GET /work` - simulates a workload (DB read, cache ops, metrics)
- `POST /chaos` - change mode (normal | slow | error) by JSON body
- `POST /db/insert?name=...` - insert an item into DB
