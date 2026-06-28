#!/usr/bin/env python3
"""
Export the currently signed-in Kiro IDE account from local cache.

The default output is a complete JSON format that Kiro-Go Plus can import.
It preserves enterprise external_idp metadata such as tokenEndpoint, issuerUrl,
scopes, clientId, refreshToken, region, and profileArn when available.
"""
from __future__ import annotations

import argparse
import base64
import json
import os
import re
import sys
import urllib.error
import urllib.request
import uuid
from datetime import date, datetime, timezone
from typing import Any

CACHE_DIR = os.path.join(os.path.expanduser("~"), ".aws", "sso", "cache")
TOKEN_FILE = os.path.join(CACHE_DIR, "kiro-auth-token.json")
DEFAULT_REGION = "us-east-1"
ARN_REGION_RE = re.compile(r"^arn:[^:]*:codewhisperer:([^:]+):[^:]+:profile/.+")


def jwt_payload(token: str) -> dict[str, Any]:
    try:
        seg = token.split(".")[1]
        seg += "=" * (-len(seg) % 4)
        payload = json.loads(base64.urlsafe_b64decode(seg.encode("ascii")))
        return payload if isinstance(payload, dict) else {}
    except Exception:
        return {}


def jwt_field(token: str, *keys: str) -> str:
    payload = jwt_payload(token)
    for key in keys:
        value = payload.get(key)
        if value:
            return str(value)
    return ""


def first_string(data: dict[str, Any], *keys: str) -> str:
    for key in keys:
        value = data.get(key)
        if isinstance(value, str) and value.strip():
            return value.strip()
    return ""


def parse_region_from_arn(profile_arn: str) -> str:
    match = ARN_REGION_RE.match((profile_arn or "").strip())
    return match.group(1) if match else ""


def normalize_auth_method(value: str) -> str:
    method = (value or "").strip()
    lower = method.lower().replace("-", "_")
    if lower in {"external_idp", "externalidp", "external"}:
        return "external_idp"
    if lower in {"idc", "builderid", "builder_id", "enterprise"}:
        return "idc"
    if lower in {"social", "google", "github"}:
        return "social"
    return method or "social"


def infer_email(tok: dict[str, Any]) -> str:
    explicit = first_string(tok, "email", "username", "userName")
    if explicit:
        return explicit
    return jwt_field(
        first_string(tok, "accessToken", "access_token"),
        "preferred_username",
        "email",
        "upn",
        "unique_name",
    )


def infer_region(tok: dict[str, Any], profile_arn: str, fallback: str) -> str:
    region = first_string(tok, "region")
    if region:
        return region
    region = parse_region_from_arn(profile_arn)
    if region:
        return region
    return fallback or DEFAULT_REGION


def fetch_profile_arn(access_token: str, region: str, timeout: float = 12.0) -> tuple[str, str]:
    if not access_token:
        return "", "missing accessToken"
    region = region or DEFAULT_REGION
    url = f"https://management.{region}.kiro.dev/ListAvailableProfiles"
    payload = json.dumps({"maxResults": 10}).encode("utf-8")
    req = urllib.request.Request(url, data=payload, method="POST")
    req.add_header("Authorization", "Bearer " + access_token)
    req.add_header("Accept", "application/json")
    req.add_header("Content-Type", "application/json")
    req.add_header("User-Agent", "aws-sdk-js/1.0.0 KiroIDE")
    req.add_header("x-amz-user-agent", "aws-sdk-js/1.0.0 KiroIDE")
    req.add_header("x-amzn-codewhisperer-optout", "true")
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            data = json.loads(resp.read().decode("utf-8"))
    except urllib.error.HTTPError as exc:
        body = exc.read().decode("utf-8", errors="replace")[:240]
        return "", f"HTTP {exc.code}: {body}"
    except Exception as exc:
        return "", str(exc)

    profiles = data.get("profiles") if isinstance(data, dict) else None
    if isinstance(profiles, list):
        for item in profiles:
            if isinstance(item, dict):
                arn = first_string(item, "arn", "profileArn", "profile_arn")
                if arn:
                    return arn, ""
    return "", "ListAvailableProfiles returned no profile"


def build_minimal_record(tok: dict[str, Any]) -> dict[str, Any]:
    return {
        "email": infer_email(tok),
        "refreshToken": first_string(tok, "refreshToken", "refresh_token"),
        "provider": first_string(tok, "provider") or "",
    }


def build_full_record(
    tok: dict[str, Any],
    region_override: str = "",
    profile_arn_override: str = "",
    fetch_profile: bool = True,
) -> tuple[dict[str, Any], str]:
    access_token = first_string(tok, "accessToken", "access_token")
    refresh_token = first_string(tok, "refreshToken", "refresh_token")
    client_id = first_string(tok, "clientId", "client_id")
    client_secret = first_string(tok, "clientSecret", "client_secret")
    auth_method = normalize_auth_method(first_string(tok, "authMethod", "auth_method"))
    provider = first_string(tok, "provider") or ("ExternalIdp" if auth_method == "external_idp" else "")
    profile_arn = profile_arn_override or first_string(tok, "profileArn", "profile_arn")
    region = region_override or infer_region(tok, profile_arn, DEFAULT_REGION)
    profile_note = ""

    if fetch_profile and not profile_arn:
        fetched, err = fetch_profile_arn(access_token, region)
        if fetched:
            profile_arn = fetched
            region = parse_region_from_arn(profile_arn) or region
        else:
            profile_note = err

    expires_at = tok.get("expiresAt", tok.get("expires_at", ""))
    token_endpoint = first_string(tok, "tokenEndpoint", "token_endpoint")
    issuer_url = first_string(tok, "issuerUrl", "issuer_url")
    scopes = first_string(tok, "scopes")
    start_url = first_string(tok, "startUrl", "start_url")
    email = infer_email(tok)

    record: dict[str, Any] = {
        "type": "kiro",
        "email": email,
        "accessToken": access_token,
        "refreshToken": refresh_token,
        "clientId": client_id,
        "clientSecret": client_secret,
        "region": region,
        "provider": provider,
        "authMethod": auth_method,
        "profileArn": profile_arn,
        "expiresAt": expires_at,
        "startUrl": start_url,
        "tokenEndpoint": token_endpoint,
        "issuerUrl": issuer_url,
        "scopes": scopes,
        "access_token": access_token,
        "refresh_token": refresh_token,
        "client_id": client_id,
        "client_secret": client_secret,
        "profile_arn": profile_arn,
        "expires_at": expires_at,
        "start_url": start_url,
        "token_endpoint": token_endpoint,
        "issuer_url": issuer_url,
        "auth_method": auth_method,
    }

    # Keep empty clientSecret keys so importers can distinguish public-client
    # external_idp accounts from incomplete exports.
    keep_empty = {"clientSecret", "client_secret"}
    record = {k: v for k, v in record.items() if v not in ("", None) or k in keep_empty}
    return record, profile_note


def expires_at_millis(value: Any) -> int:
    if isinstance(value, (int, float)):
        timestamp = int(value)
        return timestamp if timestamp > 10_000_000_000 else timestamp * 1000
    if isinstance(value, str) and value.strip():
        text = value.strip()
        if text.isdigit():
            return expires_at_millis(int(text))
        try:
            parsed = datetime.fromisoformat(text.replace("Z", "+00:00"))
            if parsed.tzinfo is None:
                parsed = parsed.replace(tzinfo=timezone.utc)
            return int(parsed.timestamp() * 1000)
        except ValueError:
            return 0
    return 0


def build_kam_rs_account(record: dict[str, Any]) -> dict[str, Any]:
    auth_method = normalize_auth_method(first_string(record, "authMethod", "auth_method"))
    provider = first_string(record, "provider") or ("ExternalIdp" if auth_method == "external_idp" else "BuilderId")
    email = first_string(record, "email")
    region = first_string(record, "region") or DEFAULT_REGION
    start_url = first_string(record, "startUrl", "start_url")
    profile_arn = first_string(record, "profileArn", "profile_arn")
    token_endpoint = first_string(record, "tokenEndpoint", "token_endpoint")
    issuer_url = first_string(record, "issuerUrl", "issuer_url")
    scopes = first_string(record, "scopes")
    access_token = first_string(record, "accessToken", "access_token")
    refresh_token = first_string(record, "refreshToken", "refresh_token")
    client_id = first_string(record, "clientId", "client_id")
    client_secret = first_string(record, "clientSecret", "client_secret")
    expires_at = expires_at_millis(record.get("expiresAt", record.get("expires_at", 0)))

    credentials = {
        "accessToken": access_token,
        "csrfToken": "",
        "refreshToken": refresh_token,
        "clientId": client_id,
        "clientSecret": client_secret,
        "region": region,
        "startUrl": start_url,
        "profileArn": profile_arn,
        "tokenEndpoint": token_endpoint,
        "issuerUrl": issuer_url,
        "scopes": scopes,
        "expiresAt": expires_at,
        "authMethod": auth_method,
        "provider": provider,
    }

    now_ms = int(datetime.now(tz=timezone.utc).timestamp() * 1000)
    account = {
        "id": first_string(record, "id") or str(uuid.uuid4()),
        "email": email,
        "nickname": email,
        "idp": "Enterprise" if auth_method == "external_idp" else provider,
        "provider": provider,
        "authMethod": auth_method,
        "auth_method": auth_method,
        "userId": first_string(record, "userId", "user_id"),
        "machineId": first_string(record, "machineId", "machine_id") or str(uuid.uuid4()),
        "credentials": credentials,
        "subscription": {"type": "", "title": ""},
        "usage": {"current": 0, "limit": 0, "percentUsed": 0, "lastUpdated": now_ms},
        "tags": [],
        "status": "active",
        "createdAt": now_ms,
        "lastUsedAt": now_ms,
        "password": None,
    }

    # Duplicate credential fields at the top level for older KAM/Kiro-rs
    # importers that inspect only account-level keys before deciding auth type.
    for key in (
        "accessToken",
        "refreshToken",
        "clientId",
        "clientSecret",
        "region",
        "startUrl",
        "profileArn",
        "tokenEndpoint",
        "issuerUrl",
        "scopes",
        "expiresAt",
    ):
        account[key] = credentials[key]
    aliases = {
        "access_token": "accessToken",
        "refresh_token": "refreshToken",
        "client_id": "clientId",
        "client_secret": "clientSecret",
        "profile_arn": "profileArn",
        "token_endpoint": "tokenEndpoint",
        "issuer_url": "issuerUrl",
        "start_url": "startUrl",
    }
    for snake_key, camel_key in aliases.items():
        account[snake_key] = account.get(camel_key, "")

    return account


def dedupe_accounts(accounts: list[dict[str, Any]], record: dict[str, Any]) -> list[dict[str, Any]]:
    rt = first_string(record, "refreshToken", "refresh_token")
    email = first_string(record, "email")
    return [
        item
        for item in accounts
        if first_string(item, "refreshToken", "refresh_token") != rt
        and not (email and first_string(item, "email") == email)
    ]


def load_existing(path: str, append: bool) -> list[dict[str, Any]]:
    if not append or not os.path.exists(path):
        return []
    try:
        with open(path, encoding="utf-8") as f:
            existing = json.load(f)
        if isinstance(existing, list):
            return [item for item in existing if isinstance(item, dict)]
        if isinstance(existing, dict) and isinstance(existing.get("accounts"), list):
            return [item for item in existing["accounts"] if isinstance(item, dict)]
    except (OSError, json.JSONDecodeError):
        pass
    return []


def safe_tail(value: str, head: int = 12, tail: int = 6) -> str:
    if not value:
        return "(empty)"
    if len(value) <= head + tail:
        return f"<len={len(value)}>"
    return f"{value[:head]}...{value[-tail:]}  (len={len(value)})"


def default_output_path(script_dir: str, fmt: str) -> str:
    if fmt == "full":
        name = "kiro-full-accounts"
    elif fmt == "kam-rs":
        name = "kiro-kam-rs-accounts"
    else:
        name = "kiro-accounts"
    return os.path.join(script_dir, f"{name}-{date.today().isoformat()}.json")


def main() -> int:
    script_dir = os.path.dirname(os.path.abspath(__file__))

    ap = argparse.ArgumentParser(description="Export signed-in Kiro IDE account to JSON")
    ap.add_argument("-o", "--out", help="output JSON file")
    ap.add_argument("--cache-file", default=TOKEN_FILE, help="path to kiro-auth-token.json")
    ap.add_argument("--append", action="store_true", help="append to existing output, deduped by email/refreshToken")
    ap.add_argument(
        "--format",
        choices=("full", "minimal", "kam-rs"),
        default="full",
        help="output format; default: full. Use kam-rs for KAM/Kiro-rs envelope with credentials and external_idp fields duplicated at top level.",
    )
    ap.add_argument("--region", default="", help="fallback Kiro region when cache has none; default: us-east-1")
    ap.add_argument("--profile-arn", default="", help="manually set profileArn")
    ap.add_argument("--no-fetch-profile", action="store_true", help="do not call ListAvailableProfiles")
    args = ap.parse_args()

    output_path = args.out or default_output_path(script_dir, args.format)

    try:
        with open(args.cache_file, encoding="utf-8") as f:
            tok = json.load(f)
    except FileNotFoundError:
        print(f"[!] Missing {args.cache_file}; sign in to Kiro IDE first.", file=sys.stderr)
        return 1
    except (OSError, json.JSONDecodeError) as exc:
        print(f"[!] Failed to read/parse cache: {exc}", file=sys.stderr)
        return 1

    if not first_string(tok, "refreshToken", "refresh_token"):
        print("[!] Cache has no refreshToken; Kiro IDE may not be fully signed in.", file=sys.stderr)
        return 1

    if args.format == "minimal":
        record = build_minimal_record(tok)
        profile_note = ""
    else:
        record, profile_note = build_full_record(
            tok,
            region_override=args.region.strip(),
            profile_arn_override=args.profile_arn.strip(),
            fetch_profile=not args.no_fetch_profile,
        )

    accounts = load_existing(output_path, args.append)
    if args.format == "kam-rs":
        kam_account = build_kam_rs_account(record)
        accounts = dedupe_accounts(accounts, kam_account)
        accounts.append(kam_account)
        output_data: Any = {
            "version": "merged",
            "exportedAt": int(datetime.now(tz=timezone.utc).timestamp() * 1000),
            "accounts": accounts,
            "groups": [],
            "tags": [],
        }
    else:
        accounts = dedupe_accounts(accounts, record)
        accounts.append(record)
        output_data = accounts

    os.makedirs(os.path.dirname(os.path.abspath(output_path)), exist_ok=True)
    with open(output_path, "w", encoding="utf-8") as f:
        json.dump(output_data, f, ensure_ascii=False, indent=2)
    os.chmod(output_path, 0o600)

    print("Exported Kiro account:")
    print(f"  format       : {args.format}")
    print(f"  email        : {record.get('email') or '(unknown)'}")
    print(f"  provider     : {record.get('provider') or ''}   authMethod={record.get('authMethod') or ''}")
    print(f"  region       : {record.get('region') or '(empty)'}")
    print(f"  profileArn   : {'present' if record.get('profileArn') else 'missing'}")
    if profile_note:
        print(f"  profile note : {profile_note}")
    print(f"  refreshToken : {safe_tail(first_string(record, 'refreshToken', 'refresh_token'))}")
    print(f"  -> {output_path}  ({len(accounts)} account(s))")
    return 0


if __name__ == "__main__":
    sys.exit(main())
