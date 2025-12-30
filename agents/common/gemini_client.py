from __future__ import annotations
from typing import Optional, Tuple, Any, Dict
from google import genai
from google.genai import types

def build_gemini_client(api_key: str) -> genai.Client:
    """Build a Gemini client with the new API."""
    return genai.Client(api_key=api_key)

def analyze_metrics_with_gemini(
    client: genai.Client,
    model: str,
    incident_context: Dict[str, Any],
    snapshot_png_bytes: Optional[bytes] = None,
    api_keys: list[str] = None,
    current_key_index: int = 0,
) -> Tuple[str, int]:
    """Analyze metrics using Gemini with the new API."""
    
    # Build the prompt
    prompt = f"""Analyze this incident based on the following context:
    
Incident ID: {incident_context.get('incident_id')}
Monitor: {incident_context.get('monitor', {}).get('title')}
Time Window: {incident_context.get('time_window_ms')}

Timeseries Data:
{incident_context.get('timeseries')}

Provide a detailed analysis of what might have caused this incident.
"""
    
    contents = [prompt]
    
    # Add image if available
    if snapshot_png_bytes:
        contents.append(types.Part.from_bytes(
            data=snapshot_png_bytes,
            mime_type="image/png"
        ))
    
    try:
        response = client.models.generate_content(
            model=model,
            contents=contents
        )
        return response.text, current_key_index
    except Exception as e:
        print(f"[gemini_client] Error with key index {current_key_index}: {e}")
        
        # Try next API key if available
        if api_keys and current_key_index < len(api_keys) - 1:
            next_index = current_key_index + 1
            print(f"[gemini_client] Retrying with key index {next_index}")
            new_client = genai.Client(api_key=api_keys[next_index])
            return analyze_metrics_with_gemini(
                client=new_client,
                model=model,
                incident_context=incident_context,
                snapshot_png_bytes=snapshot_png_bytes,
                api_keys=api_keys,
                current_key_index=next_index,
            )
        raise

def analyze_incident_with_gemini(
    client: genai.Client,
    model: str,
    incident_context: Dict[str, Any],
    logs_data: list,
    api_keys: list[str] = None,
    current_key_index: int = 0,
) -> Tuple[str, int]:
    """Analyze incident logs using Gemini."""
    
    prompt = f"""Analyze this incident based on logs and traces:
    
Incident ID: {incident_context.get('incident_id')}
Monitor: {incident_context.get('monitor', {}).get('title')}

Logs:
{logs_data}

Provide a detailed analysis of the root cause.
"""
    
    try:
        response = client.models.generate_content(
            model=model,
            contents=prompt
        )
        return response.text, current_key_index
    except Exception as e:
        print(f"[gemini_client] Error with key index {current_key_index}: {e}")
        
        # Try next API key if available
        if api_keys and current_key_index < len(api_keys) - 1:
            next_index = current_key_index + 1
            print(f"[gemini_client] Retrying with key index {next_index}")
            new_client = genai.Client(api_key=api_keys[next_index])
            return analyze_incident_with_gemini(
                client=new_client,
                model=model,
                incident_context=incident_context,
                logs_data=logs_data,
                api_keys=api_keys,
                current_key_index=next_index,
            )
        raise

def gemini_json_call(
    client: genai.Client,
    model: str,
    prompt: Optional[str] = None,
    system_instructions: Optional[str] = None,
    payload: Optional[Any] = None,
) -> Dict[str, Any]:
    """Make a JSON-formatted call to Gemini.

    Backwards-compatible helper: callers may pass either a string `prompt`
    or a structured `payload` plus optional `system_instructions`. When a
    structured payload is provided we serialize it to JSON and prepend the
    system_instructions (if any) so older call sites that pass
    `system_instructions=`/`payload=` continue to work.
    """
    import json

    if payload is not None:
        # If payload is a dict, serialize it; otherwise coerce to string.
        if isinstance(payload, (dict, list)):
            payload_str = json.dumps(payload)
        else:
            payload_str = str(payload)
        if system_instructions:
            prompt_str = system_instructions + "\n\n" + payload_str
        else:
            prompt_str = payload_str
    else:
        prompt_str = prompt or ""

    response = client.models.generate_content(
        model=model,
        contents=prompt_str,
        config=types.GenerateContentConfig(
            response_mime_type="application/json"
        ),
    )

    # Try strict JSON parse first. If Gemini returned non-strict JSON
    # (extra commentary, trailing commas, or plain text), attempt to
    # recover by extracting the first JSON object substring. If that
    # still fails, return a wrapper with the raw text so callers can
    # handle it gracefully instead of crashing.
    text = response.text
    try:
        return json.loads(text)
    except Exception:
        # attempt to find a JSON object within the text
        try:
            first = text.find("{")
            last = text.rfind("}")
            if first != -1 and last != -1 and last > first:
                cand = text[first : last + 1]
                return json.loads(cand)
        except Exception:
            pass
    # fallback: return raw response under a known key so consumers
    # can detect non-JSON output and inspect `__raw_response__`.
    return {"__raw_response__": text}