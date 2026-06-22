# Tokenio Gateway OpenAPI

`openapi.yaml` is the canonical machine-readable HTTP API contract for Tokenio Gateway.

It covers:

- public OpenAI-compatible endpoints;
- public native Anthropic, Gemini, and Ollama endpoints;
- internal API key provisioning endpoints;
- admin control-plane endpoints;
- shared auth schemes, billing headers, and error envelope schemas.

Run the local check:

```sh
bash scripts/openapi_check.sh
```

The check validates and bundles the OpenAPI document, builds static HTML docs, and compares the operation inventory against the current Tokenio Gateway route inventory.

Generated files are written to:

```text
docs/openapi/dist/tokenio-gateway-openapi.yaml
docs/openapi/dist/tokenio-gateway-openapi.json
docs/openapi/dist/index.html
```
