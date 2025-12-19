import time
import random
from fastapi import FastAPI, HTTPException
from pydantic import BaseModel
from datadog import statsd

app = FastAPI(title="Dummy Checkout Service")

# Global state to simulate incidents
STATE = {
    "mode": "normal"  # normal | slow | error
}

statsd.gauge("dummy.mode", 1 if STATE["mode"] != "normal" else 0, tags=[f"mode:{STATE['mode']}"])

class ChaosRequest(BaseModel):
    mode: str  # normal | slow | error

@app.get("/health")
def health():
    return {"status": "ok", "mode": STATE["mode"]}

@app.get("/work")
def work():
    """
    Simulates a checkout or critical request
    """
    if STATE["mode"] == "slow":
        time.sleep(random.uniform(1.5, 3.0))

    if STATE["mode"] == "error":
        if random.random() < 0.7:
            raise HTTPException(status_code=500, detail="Simulated failure")

    return {
        "result": "success",
        "mode": STATE["mode"]
    }

@app.post("/chaos")
def chaos(req: ChaosRequest):
    if req.mode not in ["normal", "slow", "error"]:
        raise HTTPException(status_code=400, detail="Invalid mode")

    STATE["mode"] = req.mode
    return {"mode": STATE["mode"]}
