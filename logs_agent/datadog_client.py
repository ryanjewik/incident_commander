from __future__ import annotations
import re
import requests
import time
from typing import Any, Dict, List, Optional, Tuple


def _dd_api_base(dd_site: str) -> str:
    # Example DD_SITE: "us5.datadoghq.com" -> "https://api.us5.datadoghq.com"
    return f"https://api.{dd_site}"


def _headers(dd_api_key: str, dd_app_key: str) -> Dict[str, str]:
    return {
        "DD-API-KEY": dd_api_key,
        "DD-APPLICATION-KEY": dd_app_key,
        "Content-Type": "application/json",
    }


def parse_related_logs_url_time_window(text_only_message: str) -> Optional[Tuple[int, int]]:
    """
    Tries to extract from_ts/to_ts (ms) from the "Related Logs" URL embedded in the webhook.
    Your payload includes these in the message block.

    Returns: (from_ms, to_ms) if found else None
    """
    # Example: ...from_ts=1766898172000&to_ts=1766898472000...
    m_from = re.search(r"from_ts=(\d{10,13})", text_only_message)
    m_to = re.search(r"to_ts=(\d{10,13})", text_only_message)
    if not (m_from and m_to):
        return None
    from_ts = int(m_from.group(1))
    to_ts = int(m_to.group(1))
    return from_ts, to_ts


def search_logs(
    *,
    dd_site: str,
    dd_api_key: str,
    dd_app_key: str,
    query: str,
    from_ms: int,
    to_ms: int,
    limit: int = 50,
) -> List[Dict[str, Any]]:
    """
    Uses Logs List/Search endpoint (v2) to retrieve individual log events.
    """
    url = f"{_dd_api_base(dd_site)}/api/v2/logs/events/search"
    body = {
        "filter": {
            "from": str(from_ms),
            "to": str(to_ms),
            "query": query,
        },
        "page": {"limit": limit},
        "sort": "timestamp",
    }
    # Retry on 429 Too Many Requests with exponential backoff to avoid
    # crashing the agent on transient rate limits. Return an empty list if
    # the request ultimately fails due to persistent rate limiting or other
    # HTTP errors so callers can continue.
    attempts = 0
    backoff = 1
    last_response = None
    while attempts < 4:
        try:
            r = requests.post(url, headers=_headers(dd_api_key, dd_app_key), json=body, timeout=30)
            last_response = r
            if r.status_code == 429:
                attempts += 1
                print(f"[datadog_client] search_logs rate-limited (429), retrying in {backoff}s")
                time.sleep(backoff)
                backoff *= 2
                continue
            r.raise_for_status()
            data = r.json()
            return data.get("data", [])
        except requests.HTTPError as he:
            # For non-429 HTTP errors, surface helpful debug info and return []
            # instead of raising, to keep the agent resilient.
            try:
                status = last_response.status_code if last_response is not None else 'n/a'
                print(f"[datadog_client] search_logs HTTPError status={status} body={getattr(last_response,'text',None)}")
            except Exception:
                pass
            return []
        except Exception as e:
            # Network/other errors: log and retry once (counted above) then return [].
            attempts += 1
            print(f"[datadog_client] search_logs error: {e}, retrying in {backoff}s (attempt {attempts})")
            time.sleep(backoff)
            backoff *= 2

    # After retries exhausted, if we still have a last response log it and return []
    if last_response is not None:
        print(f"[datadog_client] search_logs failed after retries: status={last_response.status_code} body={last_response.text}")
    else:
        print("[datadog_client] search_logs failed after retries: no response")
    return []


def aggregate_logs(
    *,
    dd_site: str,
    dd_api_key: str,
    dd_app_key: str,
    query: str,
    from_ms: int,
    to_ms: int,
    group_by: str,
    limit: int = 10,
) -> Dict[str, Any]:
    """
    Uses Logs Aggregate endpoint (v2) to compute top groups.
    """
    # Use the Logs Analytics aggregate endpoint
    url = f"{_dd_api_base(dd_site)}/api/v2/logs/analytics/aggregate"
    body = {
        "filter": {"from": str(from_ms), "to": str(to_ms), "query": query},
        "compute": [{"aggregation": "count", "metric": "@_id"}],
        "group_by": [{"facet": group_by, "limit": limit}],
    }
    # Retry on 429 Too Many Requests with exponential backoff
    attempts = 0
    backoff = 1
    while attempts < 4:
        r = requests.post(url, headers=_headers(dd_api_key, dd_app_key), json=body, timeout=30)
        if r.status_code == 429:
            attempts += 1
            print(f"[datadog_client] aggregate rate-limited (429), retrying in {backoff}s")
            time.sleep(backoff)
            backoff *= 2
            continue
        try:
            r.raise_for_status()
        except requests.HTTPError:
            # Surface helpful debugging info for non-retryable responses
            raise
        return r.json()
    # After retries, if still failing (e.g., rate-limited), log and return empty aggregation
    try:
        r.raise_for_status()
    except requests.HTTPError as e:
        print(f"[datadog_client] aggregate failed after retries: status={r.status_code} body={r.text}")
        return {}


def search_spans(
    *,
    dd_site: str,
    dd_api_key: str,
    dd_app_key: str,
    query: str,
    from_ms: int,
    to_ms: int,
    limit: int = 50,
) -> List[Dict[str, Any]]:
    """
    Uses Spans search endpoint (v2).
    """
    url = f"{_dd_api_base(dd_site)}/api/v2/spans/events/search"
    body = {
        "filter": {"from": str(from_ms), "to": str(to_ms), "query": query},
        "page": {"limit": limit},
        "sort": "timestamp",
    }
    r = requests.post(url, headers=_headers(dd_api_key, dd_app_key), json=body, timeout=30)
    try:
        r.raise_for_status()
    except requests.HTTPError:
        # Surface helpful debug info and return empty list so caller can continue
        print(f"[datadog_client] spans search failed: status={r.status_code} body={r.text}")
        return []
    data = r.json()
    return data.get("data", [])


def aggregate_spans(
    *,
    dd_site: str,
    dd_api_key: str,
    dd_app_key: str,
    query: str,
    from_ms: int,
    to_ms: int,
    group_by: str,
    limit: int = 10,
) -> Dict[str, Any]:
    """
    Spans aggregate endpoint is not always enabled in every plan.
    If it errors, caller should fall back to simple client-side grouping.
    """
    url = f"{_dd_api_base(dd_site)}/api/v2/spans/analytics/aggregate"
    body = {
        "filter": {"from": str(from_ms), "to": str(to_ms), "query": query},
        "compute": [{"aggregation": "count"}],
        "group_by": [{"facet": group_by, "limit": limit}],
    }
    r = requests.post(url, headers=_headers(dd_api_key, dd_app_key), json=body, timeout=30)
    r.raise_for_status()
    return r.json()


def download_snapshot_image(snapshot_url: str) -> Optional[bytes]:
    """
    If snapshot_url is publicly accessible (yours is), download and return bytes.
    """
    if not snapshot_url:
        return None
    r = requests.get(snapshot_url, timeout=30)
    r.raise_for_status()
    ct = r.headers.get("content-type", "")
    if "image" not in ct:
        # still return bytes; caller can decide
        return r.content
    return r.content
