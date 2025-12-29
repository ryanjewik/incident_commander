from __future__ import annotations
import json
from pathlib import Path
from datetime import datetime, timezone
from typing import Any, Dict


def write_agent_artifacts(*, incident_id: str, agent_name: str, payload: Dict[str, Any], base_dir: str = "outputs",) -> Dict[str, str]:
    ts = datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%SZ")
    out_dir = Path(base_dir) / incident_id
    out_dir.mkdir(parents=True, exist_ok=True)

    json_path = out_dir / f"{agent_name}_{ts}.json"
    txt_path = out_dir / f"{agent_name}_{ts}.txt"

    json_path.write_text(json.dumps(payload, indent=2, ensure_ascii=False), encoding="utf-8")

    lines = []
    lines.append(f"agent: {agent_name}")
    lines.append(f"incident_id: {incident_id}")
    lines.append("")
    lines.append(json.dumps(payload, indent=2, ensure_ascii=False))
    txt_path.write_text("\n".join(lines), encoding="utf-8")

    return {"json": str(json_path), "txt": str(txt_path)}