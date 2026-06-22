#!/usr/bin/env python3
"""Check that OpenAPI operations match Tokenio Gateway's route inventory."""

from __future__ import annotations

import json
import sys
from pathlib import Path

HTTP_METHODS = {"get", "put", "post", "delete", "options", "head", "patch", "trace"}

EXPECTED_OPERATIONS: set[tuple[str, str]] = {
    ("GET", "/health"),
    ("GET", "/healthz"),
    ("GET", "/readyz"),
    ("GET", "/v1/models"),
    ("POST", "/v1/chat/completions"),
    ("POST", "/v1/embeddings"),
    ("POST", "/v1/images/generations"),
    ("POST", "/v1/messages"),
    ("GET", "/v1beta/models"),
    ("POST", "/v1beta/models/{model}:generateContent"),
    ("POST", "/v1beta/models/{model}:streamGenerateContent"),
    ("POST", "/v1beta/models/{model}:embedContent"),
    ("POST", "/v1beta/models/{model}:batchEmbedContents"),
    ("GET", "/api/tags"),
    ("POST", "/api/chat"),
    ("POST", "/api/generate"),
    ("POST", "/api/embeddings"),
    ("POST", "/internal/v1/api-keys/provision"),
    ("POST", "/internal/v1/api-key-provisionings/{provisioning_id}/confirm-delivery"),
    ("GET", "/admin/v1/users"),
    ("POST", "/admin/v1/users"),
    ("POST", "/admin/v1/users/{user_id}/enable"),
    ("POST", "/admin/v1/users/{user_id}/disable"),
    ("GET", "/admin/v1/users/{user_id}/api-keys"),
    ("POST", "/admin/v1/users/{user_id}/api-keys"),
    ("POST", "/admin/v1/api-keys/{api_key_id}/revoke"),
    ("GET", "/admin/v1/api-key-provisionings"),
    ("GET", "/admin/v1/api-key-provisionings/{provisioning_id}"),
    ("GET", "/admin/v1/resellers"),
    ("POST", "/admin/v1/resellers"),
    ("PATCH", "/admin/v1/resellers/{reseller_id}"),
    ("POST", "/admin/v1/resellers/{reseller_id}/enable"),
    ("POST", "/admin/v1/resellers/{reseller_id}/disable"),
    ("GET", "/admin/v1/resellers/{reseller_id}/balance"),
    ("POST", "/admin/v1/resellers/{reseller_id}/balance/adjust"),
    ("POST", "/admin/v1/resellers/{reseller_id}/balance/set"),
    ("GET", "/admin/v1/routes"),
    ("POST", "/admin/v1/routes"),
    ("PATCH", "/admin/v1/routes/{route_id}"),
    ("POST", "/admin/v1/routes/{route_id}/enable"),
    ("POST", "/admin/v1/routes/{route_id}/disable"),
    ("GET", "/admin/v1/routes/{route_id}/cooldown"),
    ("POST", "/admin/v1/routes/{route_id}/cooldown"),
    ("DELETE", "/admin/v1/routes/{route_id}/cooldown"),
    ("GET", "/admin/v1/routes/{route_id}/price"),
    ("PUT", "/admin/v1/routes/{route_id}/price"),
    ("GET", "/admin/v1/usage-records"),
    ("GET", "/admin/v1/usage-records/{local_request_id}"),
    ("POST", "/admin/v1/usage-records/{local_request_id}/resolve/billable"),
    ("POST", "/admin/v1/usage-records/{local_request_id}/resolve/failed"),
    ("POST", "/admin/v1/usage-records/{local_request_id}/resolve/charged"),
    ("GET", "/admin/v1/billing-charge-batches"),
    ("GET", "/admin/v1/billing-charge-batches/{batch_id}"),
    ("POST", "/admin/v1/billing-charge-batches/{batch_id}/retry"),
    ("GET", "/admin/v1/route-events"),
    ("GET", "/admin/v1/telegram-alerts"),
    ("POST", "/admin/v1/telegram-alerts/{alert_id}/retry"),
    ("GET", "/admin/v1/audit-log"),
}

SOURCE_MARKERS: dict[str, list[str]] = {
    "internal/transport/httptransport/health.go": [
        'HealthPath    = "/healthz"',
        'ReadinessPath = "/readyz"',
    ],
    "internal/transport/httptransport/router.go": [
        'r.URL.Path == HealthPath || r.URL.Path == ReadinessPath || r.URL.Path == "/health"',
        'case r.URL.Path == "/v1/models"',
        'case r.URL.Path == "/v1beta/models"',
        'case r.URL.Path == "/api/tags"',
        'case isPublicLLMPath(r.URL.Path):',
        'case r.URL.Path == "/admin/v1" || strings.HasPrefix(r.URL.Path, "/admin/v1/")',
        'provisioningBasePath       = "/internal/v1/api-keys/provision"',
        'legacyProvisioningBasePath = "/internal/v1/api-key-provisionings"',
    ],
    "internal/transport/http/nativeapi/family.go": [
        'path == "/v1/chat/completions"',
        'path == "/v1/embeddings"',
        'path == "/v1/images/generations"',
        'path == "/v1/messages"',
        'path == "/api/chat"',
        'path == "/api/generate"',
        'path == "/api/embeddings"',
        'strings.HasPrefix(path, "/v1beta/models/")',
        'operation == "generateContent"',
        'operation == "streamGenerateContent"',
        'operation == "embedContent"',
        'operation == "batchEmbedContents"',
    ],
    "internal/transport/http/provisioning/router.go": [
        'basePath       = "/internal/v1/api-keys/provision"',
        'legacyBasePath = "/internal/v1/api-key-provisionings"',
        'parts[1] != "confirm-delivery"',
    ],
    "internal/transport/http/admin/router.go": [
        'basePath             = "/admin/v1"',
        'case "users":',
        'case "api-keys":',
        'case "api-key-provisionings":',
        'case "resellers":',
        'case "routes":',
        'case "usage-records":',
        'case "billing-charge-batches":',
        'case "route-events":',
        'case "telegram-alerts":',
        'case "audit-log":',
        'parts[1] == "api-keys"',
        'parts[1] == "revoke"',
        'parts[1] == "balance"',
        'parts[1] == "cooldown"',
        'parts[1] == "price"',
        'parts[1] == "retry"',
        'parts[1] == "resolve"',
        'case "billable":',
        'case "failed":',
        'case "charged":',
    ],
}


def load_operations(openapi_json: Path) -> set[tuple[str, str]]:
    with openapi_json.open("r", encoding="utf-8") as fh:
        spec = json.load(fh)

    operations: set[tuple[str, str]] = set()
    for path, path_item in spec.get("paths", {}).items():
        if not isinstance(path_item, dict):
            continue
        for method in path_item:
            method_lower = method.lower()
            if method_lower in HTTP_METHODS:
                operations.add((method_lower.upper(), path))
    return operations


def check_operation_inventory(openapi_json: Path) -> bool:
    actual = load_operations(openapi_json)
    missing = sorted(EXPECTED_OPERATIONS - actual)
    extra = sorted(actual - EXPECTED_OPERATIONS)

    ok = True
    if missing:
        ok = False
        print("OpenAPI is missing operations implemented by Tokenio Gateway:")
        for method, path in missing:
            print(f"  - {method} {path}")
    if extra:
        ok = False
        print("OpenAPI contains operations not present in the current route inventory:")
        for method, path in extra:
            print(f"  - {method} {path}")
    if ok:
        print(f"OpenAPI operation inventory OK: {len(actual)} operations")
    return ok


def check_source_markers(repo_root: Path) -> bool:
    ok = True
    for rel_path, markers in SOURCE_MARKERS.items():
        path = repo_root / rel_path
        if not path.exists():
            print(f"Source file is missing: {rel_path}")
            ok = False
            continue
        content = path.read_text(encoding="utf-8")
        for marker in markers:
            if marker not in content:
                print(f"Route source marker is missing in {rel_path}: {marker}")
                ok = False
    if ok:
        print("Route source markers OK")
    return ok


def main() -> int:
    repo_root = Path(__file__).resolve().parents[1]
    default_json = repo_root / "docs/openapi/dist/tokenio-gateway-openapi.json"
    openapi_json = Path(sys.argv[1]) if len(sys.argv) > 1 else default_json

    if not openapi_json.exists():
        print(f"OpenAPI JSON bundle is missing: {openapi_json}")
        return 2

    inventory_ok = check_operation_inventory(openapi_json)
    source_ok = check_source_markers(repo_root)
    return 0 if inventory_ok and source_ok else 1


if __name__ == "__main__":
    raise SystemExit(main())
