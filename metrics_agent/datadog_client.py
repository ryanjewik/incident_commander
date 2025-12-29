from __future__ import annotations
import time
import requests
from typing import Any, Dict, List, Optional


def _dd_api_base(dd_site: str) -> str:
    return f"https://api.{dd_site}"


def _headers(dd_api_key: str, dd_app_key: str) -> Dict[str, str]:
    return {
        "DD-API-KEY": dd_api_key,
        "DD-APPLICATION-KEY": dd_app_key,
        "Content-Type": "application/json",
    }


def query_timeseries(
    *,
    dd_site: str,
    dd_api_key: str,
    dd_app_key: str,
    query: str,
    from_ms: int,
    to_ms: int,
) -> Dict[str, Any]:
    url = f"{_dd_api_base(dd_site)}/api/v1/query"
    params = {
        "query": query,
        "from": from_ms // 1000,
        "to": to_ms // 1000,
    }
    attempts = 0
    backoff = 1
    last_response = None
    while attempts < 4:
        try:
            r = requests.get(url, headers=_headers(dd_api_key, dd_app_key), params=params, timeout=30)
            last_response = r
            if r.status_code == 429:
                attempts += 1
                print(f"[datadog_client] query_timeseries rate-limited (429), retrying in {backoff}s")
                time.sleep(backoff)
                backoff *= 2
                continue
            r.raise_for_status()
            return r.json()
        except requests.HTTPError:
            try:
                status = last_response.status_code if last_response is not None else 'n/a'
                print(f"[datadog_client] query_timeseries HTTPError status={status} body={getattr(last_response,'text',None)}")
            except Exception:
                pass
            return {}
        except Exception as e:
            attempts += 1
            print(f"[datadog_client] query_timeseries error: {e}, retrying in {backoff}s (attempt {attempts})")
            time.sleep(backoff)
            backoff *= 2
    if last_response is not None:
        print(f"[datadog_client] query_timeseries failed after retries: status={last_response.status_code} body={last_response.text}")
    else:
        print("[datadog_client] query_timeseries failed after retries: no response")
    return {}


def get_monitor_details(
    *,
    dd_site: str,
    dd_api_key: str,
    dd_app_key: str,
    monitor_id: int,
) -> Dict[str, Any]:
    url = f"{_dd_api_base(dd_site)}/api/v1/monitor/{monitor_id}"
    attempts = 0
    backoff = 1
    while attempts < 4:
        try:
            r = requests.get(url, headers=_headers(dd_api_key, dd_app_key), timeout=30)
            if r.status_code == 429:
                attempts += 1
                print(f"[datadog_client] get_monitor_details rate-limited (429), retrying in {backoff}s")
                time.sleep(backoff)
                backoff *= 2
                continue
            r.raise_for_status()
            return r.json()
        except Exception as e:
            attempts += 1
            print(f"[datadog_client] get_monitor_details error: {e}, retrying in {backoff}s (attempt {attempts})")
            time.sleep(backoff)
            backoff *= 2
    return {}


def download_snapshot_image(snapshot_url: str) -> Optional[bytes]:
    if not snapshot_url:
        return None
    r = requests.get(snapshot_url, timeout=30)
    r.raise_for_status()
    return r.content