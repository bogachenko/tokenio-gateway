#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OPENAPI_SRC="${OPENAPI_SRC:-docs/openapi/openapi.yaml}"
OPENAPI_DIST_DIR="${OPENAPI_DIST_DIR:-docs/openapi/dist}"
REDOCLY="${REDOCLY:-npx -y @redocly/cli@2}"

cd "$ROOT_DIR"
mkdir -p "$OPENAPI_DIST_DIR"

$REDOCLY lint --extends=spec --format=github-actions "$OPENAPI_SRC"
$REDOCLY bundle "$OPENAPI_SRC" --output "$OPENAPI_DIST_DIR/tokenio-gateway-openapi.yaml"
$REDOCLY bundle "$OPENAPI_SRC" --output "$OPENAPI_DIST_DIR/tokenio-gateway-openapi.json"
$REDOCLY build-docs "$OPENAPI_DIST_DIR/tokenio-gateway-openapi.yaml" --output "$OPENAPI_DIST_DIR/index.html"

python3 scripts/openapi_drift_check.py "$OPENAPI_DIST_DIR/tokenio-gateway-openapi.json"
