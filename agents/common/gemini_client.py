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
    prompt: str,
) -> Dict[str, Any]:
    """Make a JSON-formatted call to Gemini."""
    response = client.models.generate_content(
        model=model,
        contents=prompt,
        config=types.GenerateContentConfig(
            response_mime_type="application/json"
        )
    )
    
    import json
    return json.loads(response.text)