#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OPENAPI_SRC="${OPENAPI_SRC:-docs/openapi/openapi.yaml}"
PUBLIC_OPENAPI_SRC="${PUBLIC_OPENAPI_SRC:-docs/openapi/public.yaml}"
OPENAPI_DIST_DIR="${OPENAPI_DIST_DIR:-docs/openapi/dist}"
REDOCLY="${REDOCLY:-npx -y @redocly/cli@2}"

cd "$ROOT_DIR"
mkdir -p "$OPENAPI_DIST_DIR"

$REDOCLY lint --extends=spec --format=github-actions "$OPENAPI_SRC"
$REDOCLY bundle "$OPENAPI_SRC" --output "$OPENAPI_DIST_DIR/tokenio-gateway-openapi.yaml"
$REDOCLY bundle "$OPENAPI_SRC" --output "$OPENAPI_DIST_DIR/tokenio-gateway-openapi.json"
$REDOCLY build-docs "$OPENAPI_DIST_DIR/tokenio-gateway-openapi.yaml" --output "$OPENAPI_DIST_DIR/index.html"

python3 scripts/openapi_drift_check.py "$OPENAPI_DIST_DIR/tokenio-gateway-openapi.json"

if [[ -f "$PUBLIC_OPENAPI_SRC" ]]; then
  $REDOCLY lint --extends=spec --format=github-actions "$PUBLIC_OPENAPI_SRC"
  $REDOCLY bundle "$PUBLIC_OPENAPI_SRC" --output "$OPENAPI_DIST_DIR/tokenio-public-openapi.yaml"
  $REDOCLY bundle "$PUBLIC_OPENAPI_SRC" --output "$OPENAPI_DIST_DIR/tokenio-public-openapi.json"
  $REDOCLY build-docs "$OPENAPI_DIST_DIR/tokenio-public-openapi.yaml" --output "$OPENAPI_DIST_DIR/public.html"

  python3 - "$OPENAPI_DIST_DIR/tokenio-public-openapi.json" <<'PY'
import json
import sys
from pathlib import Path

path = Path(sys.argv[1])
doc = json.loads(path.read_text(encoding="utf-8"))
paths = doc.get("paths") or {}
for route in paths:
    if route.startswith("/admin/") or route.startswith("/internal/"):
        raise SystemExit(f"public OpenAPI contains private path: {route}")

security_schemes = (doc.get("components") or {}).get("securitySchemes") or {}
for forbidden in ("AdminBearerAuth", "ProvisioningServiceToken"):
    if forbidden in security_schemes:
        raise SystemExit(f"public OpenAPI contains private security scheme: {forbidden}")

print(f"Public OpenAPI OK: {len(paths)} paths")
PY
fi
