from __future__ import annotations
import re
import requests
import time
import random
from typing import Any, Dict, List, Optional, Tuple


def _dd_api_base(dd_site: str) -> str:
    return f"https://api.{dd_site}"


def _headers(dd_api_key: str, dd_app_key: str) -> Dict[str, str]:
    return {
        "DD-API-KEY": dd_api_key,
        "DD-APPLICATION-KEY": dd_app_key,
        "Content-Type": "application/json",
    }


def parse_related_logs_url_time_window(text_only_message: str) -> Optional[Tuple[int, int]]:
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
    attempts = 0
    backoff = 1
    last_response = None
    while attempts < 4:
        try:
            r = requests.post(url, headers=_headers(dd_api_key, dd_app_key), json=body, timeout=30)
            last_response = r
            if r.status_code == 429:
                attempts += 1
                ra = r.headers.get("Retry-After")
                try:
                    wait = float(ra) if ra is not None else backoff
                except Exception:
                    wait = backoff
                wait = wait + random.uniform(0, 1)
                print(f"[datadog_client] search_logs rate-limited (429), retrying in {wait:.1f}s")
                time.sleep(wait)
                backoff = min(backoff * 2, 60)
                continue
            r.raise_for_status()
            data = r.json()
            return data.get("data", [])
        except requests.HTTPError as he:
            try:
                status = last_response.status_code if last_response is not None else 'n/a'
                print(f"[datadog_client] search_logs HTTPError status={status} body={getattr(last_response,'text',None)}")
            except Exception:
                pass
            return []
        except Exception as e:
            attempts += 1
            wait = backoff + random.uniform(0, 0.5)
            print(f"[datadog_client] search_logs error: {e}, retrying in {wait:.1f}s (attempt {attempts})")
            time.sleep(wait)
            backoff = min(backoff * 2, 60)

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
    url = f"{_dd_api_base(dd_site)}/api/v2/logs/analytics/aggregate"
    body = {
        "filter": {"from": str(from_ms), "to": str(to_ms), "query": query},
        "compute": [{"aggregation": "count", "metric": "@_id"}],
        "group_by": [{"facet": group_by, "limit": limit}],
    }
    attempts = 0
    backoff = 1
    while attempts < 4:
        r = requests.post(url, headers=_headers(dd_api_key, dd_app_key), json=body, timeout=30)
        if r.status_code == 429:
            attempts += 1
            ra = r.headers.get("Retry-After")
            try:
                wait = float(ra) if ra is not None else backoff
            except Exception:
                wait = backoff
            wait = wait + random.uniform(0, 1)
            print(f"[datadog_client] aggregate rate-limited (429), retrying in {wait:.1f}s")
            time.sleep(wait)
            backoff = min(backoff * 2, 60)
            continue
        try:
            r.raise_for_status()
        except requests.HTTPError:
            raise
        return r.json()
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
    url = f"{_dd_api_base(dd_site)}/api/v2/spans/events/search"
    body = {
        "filter": {"from": str(from_ms), "to": str(to_ms), "query": query},
        "page": {"limit": limit},
        "sort": "timestamp",
    }
    attempts = 0
    backoff = 1
    last_response = None
    while attempts < 4:
        try:
            r = requests.post(url, headers=_headers(dd_api_key, dd_app_key), json=body, timeout=30)
            last_response = r
            if r.status_code == 429:
                attempts += 1
                ra = r.headers.get("Retry-After")
                try:
                    wait = float(ra) if ra is not None else backoff
                except Exception:
                    wait = backoff
                wait = wait + random.uniform(0, 1)
                print(f"[datadog_client] search_logs rate-limited (429), retrying in {wait:.1f}s")
                time.sleep(wait)
                backoff = min(backoff * 2, 60)
                continue

            if r.status_code == 400:
                try:
                    wrapped = {"data": body}
                    rw = requests.post(url, headers=_headers(dd_api_key, dd_app_key), json=wrapped, timeout=30)
                    last_response = rw
                    rw.raise_for_status()
                    return rw.json().get("data", [])
                except Exception:
                    print(f"[datadog_client] spans search 400, attempted wrapped body; got status={getattr(rw,'status_code',None)} body={getattr(rw,'text',None)}")
                    return []

            r.raise_for_status()
            data = r.json()
            return data.get("data", [])
        except requests.HTTPError as he:
            try:
                status = last_response.status_code if last_response is not None else 'n/a'
                print(f"[datadog_client] spans search HTTPError status={status} body={getattr(last_response,'text',None)}")
            except Exception:
                pass
            return []
        except Exception as e:
            attempts += 1
            print(f"[datadog_client] spans search error: {e}, retrying in {backoff}s (attempt {attempts})")
            time.sleep(backoff)
            backoff *= 2

    if last_response is not None:
        print(f"[datadog_client] spans search failed after retries: status={last_response.status_code} body={last_response.text}")
    else:
        print("[datadog_client] spans search failed after retries: no response")
    return []


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
    if not snapshot_url:
        return None
    r = requests.get(snapshot_url, timeout=30)
    r.raise_for_status()
    ct = r.headers.get("content-type", "")
    if "image" not in ct:
        return r.content
    return r.content
