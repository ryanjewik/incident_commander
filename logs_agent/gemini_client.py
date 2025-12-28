from __future__ import annotations
import json
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
    """
    We instruct Gemini to return STRICT JSON with explicit "reasoning".
    """
    system_instructions = (
        "You are an incident response assistant. "
        "Return STRICT JSON only (no markdown). "
        "Include a 'reasoning' field explaining why you believe your hypotheses/actions are correct. "
        "Be concrete: cite which evidence (logs/traces/aggregations) led you there."
    )

    prompt = {
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

    # Build a plain-text prompt for compatibility across genai versions.
    prompt_text = system_instructions + "\n\n" + json.dumps(prompt)
    # Try a sequence of call patterns to accommodate different genai SDK versions
    resp = None
    call_attempts = []
    # Common client.generate signatures
    call_attempts.append(lambda: client.generate(model=model, prompt=prompt_text))
    call_attempts.append(lambda: client.generate(model=model, input=prompt_text))
    call_attempts.append(lambda: client.generate(prompt=prompt_text, model=model))
    # Module-level convenience function (older/newer variants)
    call_attempts.append(lambda: getattr(genai, "generate", lambda **kw: (_ for _ in ()).throw(Exception("no-generate")))(model=model, prompt=prompt_text))

    # Try a models.generate_content style (advanced SDKs)
    def try_models_generate():
        # Build a simple content object expected by google-genai 1.x
        try:
            # For google-genai 1.x the `contents` parameter accepts simple
            # strings or `Part` objects. Passing a raw string is the simplest
            # compatible shape.
            return client.models.generate_content(model=model, contents=[prompt_text])
        except Exception:
            raise

    call_attempts.append(try_models_generate)

    last_exc = None
    for attempt in call_attempts:
        try:
            resp = attempt()
            if resp is not None:
                break
        except Exception as e:
            last_exc = e
            continue

    if resp is None:
        # Try older GenerativeModel class if present
        gm_cls = getattr(genai, "GenerativeModel", None)
        if gm_cls is not None:
            try:
                model_obj = gm_cls(model)
                resp = model_obj.generate_content(prompt_text)
            except Exception as e:
                last_exc = e

    if resp is None:
        # No compatible API found
        raw_err = str(last_exc) if last_exc is not None else "no compatible genai API found"
        return {
            "summary": "Gemini SDK unsupported in container.",
            "raw_error": raw_err,
            "reasoning": "Please install a compatible google-genai version or update the client code.",
        }

    # Extract text from response in a compatible way
    def _extract_text(resp_obj: Any) -> str:
        for attr in ("text", "content"):
            v = getattr(resp_obj, attr, None)
            if v:
                return v
        # candidates / output arrays
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
        # try to_dict or serialize
        if hasattr(resp_obj, "to_dict"):
            try:
                return json.dumps(resp_obj.to_dict())
            except Exception:
                pass
        return str(resp_obj)

    text = _extract_text(resp)
    # Strip common Markdown code fences (```json ... ``` or ``` ... ```) that
    # models sometimes wrap outputs in.
    stripped = text.strip()
    if stripped.startswith('```'):
        # remove the starting ``` and optional language token
        parts = stripped.split('\n')
        # If the first line is ```json or ``` , drop it
        if parts[0].startswith('```'):
            parts = parts[1:]
        # If the last line is a closing fence, drop it
        if parts and parts[-1].strip().startswith('```'):
            parts = parts[:-1]
        stripped = '\n'.join(parts).strip()

    try:
        return json.loads(stripped)
    except Exception:
        return {"summary": "Gemini did not return valid JSON.", "raw": text, "reasoning": "N/A"}
