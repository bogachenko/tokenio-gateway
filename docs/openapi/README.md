# Tokenio Gateway OpenAPI

`openapi.yaml` is the canonical machine-readable HTTP API contract for Tokenio Gateway.

It covers:

- public OpenAI-compatible endpoints;
- public native Anthropic, Gemini, and Ollama endpoints;
- internal API key provisioning endpoints;
- admin control-plane endpoints;
- shared auth schemes, billing headers, and error envelope schemas.

`public.yaml` is the customer-facing OpenAPI contract for API Docs.

It contains only public model access endpoints:

- OpenAI-compatible `/v1/*` endpoints;
- Anthropic-compatible `/v1/messages`;
- Gemini-compatible `/v1beta/*` endpoints;
- Ollama-compatible `/api/*` endpoints;
- public response headers and error envelope schemas.

Use `public.yaml` for landing-page API Docs. Use `openapi.yaml` only as the full gateway contract.

Run the local check:

```sh
bash scripts/openapi_check.sh
```

The check validates and bundles both OpenAPI documents, builds static HTML docs, and verifies that the public OpenAPI bundle contains only public API paths.

Generated files are written to:

```text
docs/openapi/dist/tokenio-gateway-openapi.yaml
docs/openapi/dist/tokenio-gateway-openapi.json
docs/openapi/dist/index.html
docs/openapi/dist/tokenio-public-openapi.yaml
docs/openapi/dist/tokenio-public-openapi.json
docs/openapi/dist/public.html
```
