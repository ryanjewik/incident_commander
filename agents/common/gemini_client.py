from __future__ import annotations

import json
import re
import time
import random
from typing import Any, Dict, Optional

from google import genai
from google.genai import types


def build_gemini_client(api_key: str) -> genai.Client:
    return genai.Client(api_key=api_key)


def analyze_incident_with_gemini(
    *,
    client: genai.Client,
    model: str,
    incident_context: Dict[str, Any],
    snapshot_png_bytes: Optional[bytes],
) -> Dict[str, Any]:
    system_instructions = (
        "You are an incident response assistant. "
        "Return STRICT JSON only (no markdown, no code fences). "
        "Include a 'reasoning' field explaining why you believe your hypotheses/actions are correct. "
        "Be concrete: cite which evidence (logs/traces/aggregations) led you there."
    )

    prompt_obj = {
        "task": "Analyze incident using logs + traces context and propose next actions.",
        "output_schema": {
            "summary": "string",
            "impact_guess": "string",
            "top_findings": ["string"],
            "evidence": [{"type": "string", "detail": "string"}],
            "hypotheses_ranked": [{"hypothesis": "string", "confidence": "number", "why": "string"}],
            "recommended_actions_ranked": [{"action": "string", "priority": "string", "why": "string"}],
            "queries_used": [{"system": "string", "query": "string"}],
            "reasoning": "string",
        },
        "context": incident_context,
    }

    prompt_text = system_instructions + "\n\n" + json.dumps(prompt_obj, ensure_ascii=False)

    def _make_text_part(text: str):
        try:
            return types.Part.from_text(text)
        except TypeError:
            try:
                return types.Part(text=text)
            except Exception:
                return text
        except Exception:
            return text

    def _make_bytes_part(b: bytes):
        try:
            return types.Part.from_bytes(data=b, mime_type="image/png")
        except Exception:
            try:
                return types.Part(data=b, mime_type="image/png")
            except Exception:
                return b

    if snapshot_png_bytes:
        contents = [
            _make_text_part(prompt_text),
            _make_bytes_part(snapshot_png_bytes),
        ]
    else:
        contents = [_make_text_part(prompt_text)]

    config = None
    try:
        config = types.GenerateContentConfig(
            temperature=0.2,
            response_mime_type="application/json",
        )
    except Exception:
        config = None

    resp = None
    attempts = 0
    max_attempts = 3
    while attempts < max_attempts:
        try:
            if config is not None:
                resp = client.models.generate_content(model=model, contents=contents, config=config)
            else:
                resp = client.models.generate_content(model=model, contents=contents)
            break
        except Exception as e:
            s = str(e)
            if "RESOURCE_EXHAUSTED" in s or "429" in s or "quota" in s.lower() or "RATE_LIMIT_EXCEEDED" in s:
                m = re.search(r"retryDelay[^0-9]*([0-9]+)s", s)
                if m:
                    wait = int(m.group(1))
                else:
                    wait = min(2 ** attempts, 60)
                wait = wait + random.uniform(0, 1)
                attempts += 1
                print(f"[gemini_client] rate-limited/quota (attempt {attempts}), retrying in {wait:.1f}s: {e}")
                time.sleep(wait)
                continue
            return {
                "summary": "Gemini request failed.",
                "raw_error": s,
                "reasoning": "Gemini SDK call raised an exception.",
            }

    if resp is None:
        return {
            "summary": "Gemini request failed after retries (quota or rate limit).",
            "raw_error": s if 's' in locals() else "unknown",
            "reasoning": "Quota exhausted or persistent rate limiting. Retry later.",
        }

    text = getattr(resp, "text", None)
    if not text:
        try:
            cands = getattr(resp, "candidates", None) or []
            if cands:
                content = getattr(cands[0], "content", None)
                if content and getattr(content, "parts", None):
                    parts = content.parts
                    if parts:
                        text = getattr(parts[0], "text", None) or str(parts[0])
        except Exception:
            text = None

    if not text:
        text = str(resp)

    return _safe_json_parse(text)


def _safe_json_parse(text: str) -> Dict[str, Any]:
    if not isinstance(text, str):
        return {"summary": "Gemini returned non-text output.", "raw": str(text), "reasoning": "N/A"}

    stripped = text.strip()
    if stripped.startswith("```"):
        parts = stripped.split("\n")
        if parts and parts[0].startswith("```"):
            parts = parts[1:]
        if parts and parts[-1].strip().startswith("```"):
            parts = parts[:-1]
        stripped = "\n".join(parts).strip()

    try:
        return json.loads(stripped)
    except Exception:
        return {"summary": "Gemini did not return valid JSON.", "raw": text, "reasoning": "N/A"}
    
def _extract_text(resp_obj: Any) -> str:
    for attr in ("text", "content"):
        v = getattr(resp_obj, attr, None)
        if v:
            return v
    cand = getattr(resp_obj, "candidates", None)
    if cand:
        try:
            return getattr(cand[0], "output", None) or getattr(cand[0], "text", None) or str(cand[0])
        except Exception:
            pass
    out = getattr(resp_obj, "output", None)
    if out:
        try:
            if isinstance(out, (list, tuple)):
                return out[0].get("content") if isinstance(out[0], dict) else str(out[0])
            if isinstance(out, dict):
                return out.get("content") or str(out)
        except Exception:
            pass
    if hasattr(resp_obj, "to_dict"):
        try:
            return json.dumps(resp_obj.to_dict())
        except Exception:
            pass
    return str(resp_obj)

def gemini_json_call(*, client: genai.Client, model: str, system_instructions: str, payload: Dict[str, Any]) -> Dict[str, Any]:
    """
    Sends (instructions + payload JSON) and parses STRICT JSON response.
    """
    prompt_text = system_instructions + "\n\n" + json.dumps(payload)

    # Best-compat path with google-genai 1.x (what you're pinned to)
    resp = client.models.generate_content(model=model, contents=[prompt_text])
    text = _extract_text(resp)

    stripped = text.strip()
    if stripped.startswith("```"):
        parts = stripped.split("\n")
        if parts and parts[0].startswith("```"):
            parts = parts[1:]
        if parts and parts[-1].strip().startswith("```"):
            parts = parts[:-1]
        stripped = "\n".join(parts).strip()

    try:
        return json.loads(stripped)
    except Exception:
        return {
            "summary": "Gemini did not return valid JSON.",
            "raw": text,
            "reasoning": "N/A",
        }
