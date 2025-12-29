from __future__ import annotations
import json
import time
from typing import Any, Dict, Optional

from google import genai
from google.genai import types


def build_gemini_client(api_key: str) -> genai.Client:
    return genai.Client(api_key=api_key)


def analyze_metrics_with_gemini(
    *,
    client: genai.Client,
    model: str,
    incident_context: Dict[str, Any],
    snapshot_png_bytes: Optional[bytes],
    api_keys: list[str],
    current_key_index: int = 0,
) -> tuple[Dict[str, Any], int]:
    system_instructions = (
        "You are a metrics analysis assistant for incident response. "
        "Return STRICT JSON only (no markdown). "
        "Include a 'reasoning' field explaining why metrics are abnormal. "
        "Focus on: threshold breaches, sudden spikes/drops, rate of change anomalies, baseline deviations."
    )

    prompt = {
        "task": "Analyze incident metrics and identify what looks abnormal.",
        "output_schema": {
            "summary": "string",
            "abnormal_metrics": [{"metric": "string", "reason": "string", "severity": "string"}],
            "baseline_comparison": "string",
            "impact_estimate": "string",
            "recommended_actions": [{"action": "string", "priority": "string"}],
            "reasoning": "string",
        },
        "context": incident_context,
    }

    prompt_text = system_instructions + "\n\n" + json.dumps(prompt)
    
    keys_tried = 0
    max_keys = len(api_keys)
    
    while keys_tried < max_keys:
        key_idx = (current_key_index + keys_tried) % max_keys
        current_api_key = api_keys[key_idx]
        current_client = build_gemini_client(current_api_key)
        
        for attempt in range(2):
            time.sleep(1.0)
            
            try:
                contents = [prompt_text]
                if snapshot_png_bytes:
                    contents = [
                        types.Part.from_bytes(data=snapshot_png_bytes, mime_type="image/png"),
                        prompt_text
                    ]
                
                response = current_client.models.generate_content(
                    model=model,
                    contents=contents
                )
                
                text = response.text if hasattr(response, 'text') else str(response)
                stripped = text.strip()
                
                if stripped.startswith('```'):
                    parts = stripped.split('\n')
                    if parts[0].startswith('```'):
                        parts = parts[1:]
                    if parts and parts[-1].strip().startswith('```'):
                        parts = parts[:-1]
                    stripped = '\n'.join(parts).strip()

                try:
                    result = json.loads(stripped)
                    next_key_idx = (key_idx + 1) % max_keys
                    return result, next_key_idx
                except json.JSONDecodeError:
                    result = {"summary": "Gemini did not return valid JSON.", "raw": text, "reasoning": "N/A"}
                    next_key_idx = (key_idx + 1) % max_keys
                    return result, next_key_idx
                    
            except Exception as e:
                err_str = str(e).lower()
                if "429" in err_str or "quota" in err_str or "resource_exhausted" in err_str:
                    print(f"[gemini_client] API key {key_idx + 1} rate limited, attempt {attempt + 1}/2: {e}")
                    if attempt == 0:
                        time.sleep(2.0)
                else:
                    print(f"[gemini_client] API key {key_idx + 1} error, attempt {attempt + 1}/2: {e}")
                    if attempt == 0:
                        time.sleep(1.0)
        
        print(f"[gemini_client] API key {key_idx + 1} exhausted after 2 attempts, rotating to next key")
        keys_tried += 1
    
    return {
        "summary": "All Gemini API keys exhausted.",
        "raw_error": "All API keys failed after multiple attempts",
        "reasoning": "Please check API quotas or add more keys.",
    }, current_key_index