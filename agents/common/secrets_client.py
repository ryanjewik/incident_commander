from __future__ import annotations
import os
import time
import threading
from typing import Optional, Dict
import requests
import os
import json

try:
    import firebase_admin
    from firebase_admin import credentials as fb_credentials, auth as fb_auth
    _FIREBASE_AVAILABLE = True
except Exception:
    _FIREBASE_AVAILABLE = False

class SecretsClient:
    """Fetches decrypted secrets from backend and caches them in-memory with TTL.

    Usage:
        sc = SecretsClient(backend_url="http://backend:8080", agent_token=os.getenv('AGENT_AUTH_TOKEN'))
        keys = sc.get_datadog_keys(org_id)
    """

    def __init__(self, backend_url: Optional[str] = None, agent_token: Optional[str] = None, ttl: int = 300):
        self.backend_url = backend_url or os.getenv("BACKEND_URL", "http://backend:8080")
        # Prefer a Firebase ID token if available; fall back to legacy AGENT_AUTH_TOKEN
        self.agent_token = agent_token or os.getenv("AGENT_ID_TOKEN") or os.getenv("AGENT_AUTH_TOKEN")
        self.ttl = int(os.getenv("SECRETS_CACHE_TTL", ttl))
        self._cache: Dict[str, Dict] = {}
        self._lock = threading.RLock()
        
        if not self.agent_token:
            print("[secrets_client] WARNING: No authentication token found. Set AGENT_ID_TOKEN or AGENT_AUTH_TOKEN")

    def _fetch(self, org_id: str) -> Optional[Dict[str, str]]:
        url = f"{self.backend_url}/internal/orgs/{org_id}/secrets"
        headers = {"Authorization": f"Bearer {self.agent_token}"} if self.agent_token else {}
        
        if not self.agent_token:
            print(f"[secrets_client] ERROR: Cannot fetch secrets for org={org_id} - no authentication token available")
            return None
            
        try:
            r = requests.get(url, headers=headers, timeout=5)
            r.raise_for_status()
            return r.json()
        except requests.exceptions.HTTPError as e:
            if e.response.status_code == 401:
                print(f"[secrets_client] authentication failed for org={org_id}: {e}")
                print(f"[secrets_client] hint: ensure AGENT_ID_TOKEN or AGENT_AUTH_TOKEN is valid")
            else:
                print(f"[secrets_client] fetch error for org={org_id}: {e}")
            return None
        except Exception as e:
            print(f"[secrets_client] fetch error for org={org_id}: {e}")
            return None

    def _ensure_id_token(self) -> Optional[str]:
        """Ensure we have a valid Firebase ID token. If AGENT_ID_TOKEN is not set,
        attempt to mint a custom token using the service account and exchange it
        for an ID token using the Firebase Identity Toolkit REST API.
        Requires: FIREBASE_CREDENTIALS_PATH and FIREBASE_WEB_API_KEY environment vars.
        """
        # If already set, return
        if self.agent_token:
            return self.agent_token

        # Need firebase admin to create custom token
        creds_path = os.getenv("FIREBASE_CREDENTIALS_PATH") or os.getenv("GOOGLE_APPLICATION_CREDENTIALS")
        web_api_key = os.getenv("FIREBASE_WEB_API_KEY")
        uid = os.getenv("AGENT_ID_UID", "agent-service")

        if not _FIREBASE_AVAILABLE:
            print("[secrets_client] firebase_admin not available; cannot mint custom token")
            print("[secrets_client] hint: install firebase-admin or set AGENT_AUTH_TOKEN directly")
            return None

        if not creds_path or not web_api_key:
            print("[secrets_client] missing FIREBASE_CREDENTIALS_PATH or FIREBASE_WEB_API_KEY; cannot mint token")
            print("[secrets_client] hint: set these environment variables or use AGENT_AUTH_TOKEN")
            return None

        try:
            # Initialize firebase app once
            if not firebase_admin._apps:
                cred = fb_credentials.Certificate(creds_path)
                firebase_admin.initialize_app(cred)

            custom_token = fb_auth.create_custom_token(uid)
            if isinstance(custom_token, bytes):
                custom_token = custom_token.decode("utf-8")

            # Exchange custom token for ID token
            url = f"https://identitytoolkit.googleapis.com/v1/accounts:signInWithCustomToken?key={web_api_key}"
            payload = {"token": custom_token, "returnSecureToken": True}
            r = requests.post(url, json=payload, timeout=5)
            r.raise_for_status()
            data = r.json()
            id_token = data.get("idToken")
            expires_in = int(data.get("expiresIn", "3600"))
            if id_token:
                # cache token and set TTL slightly less than expiry
                with self._lock:
                    now = time.time()
                    self._cache["__agent_id_token__"] = {"value": id_token, "expires_at": now + expires_in - 60}
                    self.agent_token = id_token
                print("[secrets_client] successfully minted and cached Firebase ID token")
                return id_token
            else:
                print("[secrets_client] failed to obtain ID token from Firebase response")
                return None
        except Exception as e:
            print(f"[secrets_client] failed to mint/exchange firebase token: {e}")
            print(f"[secrets_client] hint: check FIREBASE_CREDENTIALS_PATH and FIREBASE_WEB_API_KEY are correct")
            return None

    def get_datadog_keys(self, org_id: str) -> Optional[Dict[str, str]]:
        now = time.time()
        with self._lock:
            ent = self._cache.get(org_id)
            if ent and ent.get("expires_at", 0) > now:
                return ent.get("value")

        # If we don't have an agent id token, try to mint/exchange one
        if not self.agent_token:
            print("[secrets_client] attempting to mint Firebase ID token...")
            self._ensure_id_token()

        # fetch outside lock
        data = self._fetch(org_id)
        if not data:
            print(f"[secrets_client] failed to retrieve secrets for org={org_id}")
            return None
        val = {"datadog_api_key": data.get("datadog_api_key"), "datadog_app_key": data.get("datadog_app_key")}
        with self._lock:
            self._cache[org_id] = {"value": val, "expires_at": now + self.ttl}
        return val