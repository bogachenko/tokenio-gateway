# AGENTS.md

## Source of truth

Before making code or architecture changes, read the relevant specification documents.

Start with:

- `docs/spec/000-tokenio-gateway.ru.md`

Then read the documents for the layer you are changing:

- External API: `docs/spec/010-external-api.ru.md`
- Auth and billing identity: `docs/spec/020-auth-and-billing-identity.ru.md`
- Routing, resellers, provider types: `docs/spec/030-routing-and-resellers.ru.md`
- Pricing, usage extraction, token accounting: `docs/spec/040-pricing-and-usage.ru.md`
- Local ledger and auto-charge: `docs/spec/050-ledger-and-auto-charge.ru.md`
- Admin API: `docs/spec/060-admin-api.ru.md`
- Database schema: `docs/spec/070-database-schema.ru.md`
- Error model: `docs/spec/080-error-model.ru.md`
- Runtime configuration: `docs/spec/090-configuration.ru.md`

For architecture-changing decisions, also read:

- `docs/adr/`

`README.md` is not the source of truth for behavior. It is only a project overview and run guide.

## Non-negotiable invariants

- Provider-specific behavior must not be implemented in generic gateway layers.
- Every upstream provider is represented through `provider_type`, `reseller`, `route`, `api_family`, `endpoint_kind` and `capabilities`.
- Client auth is `Authorization: Bearer sk_...`.
- Client JWT auth is not part of the public API.
- Billing JWT is internal only.
- Request body must not be converted.
- Response body must not be converted.
- Fallback is allowed only inside the same `api_family` and `endpoint_kind`.
- Route key is `api_family + endpoint_kind + client_model`.
- Public `/billing/flush` must not be reintroduced.
- Billing metadata must be returned through headers.
- Reseller API keys are loaded through `api_key_env`.

## Before coding

For every code change:

1. Identify the layer being changed.
2. Read `docs/spec/000-tokenio-gateway.ru.md`.
3. Read the specific spec document for that layer.
4. Check relevant ADRs if the change affects architecture.
5. Do not add provider-specific hacks to generic layers.
6. Do not add fallback/legacy/MVP paths.
7. Run:

```bash
gofmt -w .
go test ./...
```
