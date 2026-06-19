# Tokenio Gateway implementation matrix

Статус документа: reconciled 2026-06-18 against `docs/spec/*`, `docs/adr/*`, package tree, wiring and automated test paths visible in the current repository snapshot.

Source of truth: `docs/spec/*`, `docs/adr/*`, executable tests and the current GitHub revision.

## Status values

| Status | Meaning |
|---|---|
| `specified` | Нормативный contract однозначно закреплён в spec/ADR. |
| `implemented` | Production implementation exists and is wired, but final automated evidence is incomplete or not yet recorded here. |
| `verified` | Implementation is backed by concrete automated test paths and verification command. |
| `pending` | Requirement is approved, but production implementation or executable evidence is still missing. |
| `unsupported` | Capability is explicitly excluded by the current specification. |

Interface, struct, repository method or constructor alone is not implementation evidence. A row may be marked `verified` only when it has implementation path, wiring path, test path and executable command.

## Reconciled implementation status

| Requirement group | Spec / ADR | Implementation path | Wiring path | Test path | Verification command | Status |
|---|---|---|---|---|---|---|
| Application dependency direction | `docs/adr/0001-go-layering.md` | `internal/domain`, `internal/application/*`, `internal/ports`, `internal/infrastructure/*`, `internal/transport/*` | Repository package layout and compile-time imports | `internal/architecture/dependencies_test.go`, `internal/application/architecture_test.go`, `internal/application/*/architecture_test.go`, `internal/application/llmrequest/layering_test.go` | `go test ./internal/architecture ./internal/application/...` | `verified` |
| Storage and migration contract | `docs/spec/070-database-schema.ru.md`, `docs/spec/090-configuration.ru.md`, `docs/adr/0003-migration-execution-policy.md` | `db/migrations/*.sql`, `db/migrations/embed.go`, `internal/infrastructure/postgres/migrations.go` | `cmd/migrate/main.go`, `internal/app/migrate.go`, `internal/app/runtime.go` | `internal/infrastructure/postgres/migrations_*_test.go`, `internal/infrastructure/postgres/*_integration_test.go`, `internal/app/migrate_test.go`, `internal/app/runtime_test.go` | `go test ./internal/infrastructure/postgres ./internal/app` | `verified` |
| OpenAI-compatible public contract | `docs/spec/010-external-api.ru.md`, `docs/spec/020-auth-and-billing-identity.ru.md`, `docs/spec/080-error-model.ru.md` | `internal/transport/http/publicapi`, `internal/application/openaicompatrequest`, `internal/infrastructure/requestmeta/openaicompat`, `internal/infrastructure/forwarding/openaicompat` | `internal/app/transport.go`, `internal/app/forwarding.go`, `internal/app/public_llm_http_composition_test.go` | `internal/transport/http/publicapi/*_test.go`, `internal/transport/httptransport/*_test.go`, `internal/application/openaicompatrequest/*_test.go`, `internal/infrastructure/requestmeta/openaicompat/*_test.go`, `internal/infrastructure/forwarding/openaicompat/*_test.go` | `go test ./internal/transport/http/... ./internal/transport/httptransport ./internal/application/openaicompatrequest ./internal/infrastructure/requestmeta/openaicompat ./internal/infrastructure/forwarding/openaicompat ./internal/app` | `verified` |
| Model catalog and public pricing | `docs/spec/010-external-api.ru.md`, `docs/spec/030-routing-and-resellers.ru.md`, `docs/spec/040-pricing-and-usage.ru.md` | `internal/application/modelcatalog`, `internal/application/pricing`, `internal/app/model_catalog_public_pricing_calculator.go`, `internal/infrastructure/postgres/model_catalog_route_repository_test.go` | `internal/app/application.go`, `internal/app/transport.go`, `internal/transport/http/publicapi/router.go` | `internal/application/modelcatalog/service_test.go`, `internal/application/pricing/*_test.go`, `internal/app/model_catalog_public_pricing_calculator_test.go`, `internal/infrastructure/postgres/model_catalog_route_repository_test.go` | `go test ./internal/application/modelcatalog ./internal/application/pricing ./internal/app ./internal/infrastructure/postgres` | `verified` |
| Operational routing policy | `docs/spec/030-routing-and-resellers.ru.md`, `docs/spec/080-error-model.ru.md`, `docs/spec/090-configuration.ru.md` | `internal/application/routing`, `internal/application/llmrequest`, `internal/infrastructure/routecapacity`, `internal/infrastructure/postgres/route_*`, `internal/infrastructure/postgres/forwarding_attempt_store.go` | `internal/app/routing_policy.go`, `internal/app/llmrequest_route_selector.go`, `internal/app/llmrequest_forwarding_executor.go`, `internal/app/forwarding_attempt_recovery_observer.go`, `internal/app/worker.go` | `internal/application/routing/*_test.go`, `internal/application/llmrequest/routing_*_test.go`, `internal/application/llmrequest/route_*_test.go`, `internal/infrastructure/routecapacity/*_test.go`, `internal/infrastructure/postgres/route_*_test.go`, `internal/app/routing_policy_test.go`, `internal/app/forwarding_attempt_recovery_worker_test.go` | `go test ./internal/application/routing ./internal/application/llmrequest ./internal/infrastructure/routecapacity ./internal/infrastructure/postgres ./internal/app` | `verified` |
| Durable ledger and billing auto-charge | `docs/spec/040-pricing-and-usage.ru.md`, `docs/spec/050-ledger-and-auto-charge.ru.md`, `docs/spec/090-configuration.ru.md` | `internal/application/ledger`, `internal/application/billing`, `internal/infrastructure/postgres/usage_*`, `internal/infrastructure/postgres/billing_*`, `internal/infrastructure/billing/*` | `internal/app/billing.go`, `internal/app/llmrequest_auto_charger.go`, `internal/app/billing_recovery_observer.go`, `internal/worker/billingrecovery` | `internal/application/ledger/*_test.go`, `internal/application/billing/*_test.go`, `internal/infrastructure/postgres/usage_*_test.go`, `internal/infrastructure/postgres/billing_*_test.go`, `internal/app/billing_*_test.go`, `internal/worker/billingrecovery/*_test.go` | `go test ./internal/application/ledger ./internal/application/billing ./internal/infrastructure/postgres ./internal/infrastructure/billing/... ./internal/app ./internal/worker/billingrecovery` | `verified` |
| Admin API core and provisioning | `docs/spec/021-api-key-provisioning.ru.md`, `docs/spec/060-admin-api.ru.md`, `docs/spec/080-error-model.ru.md` | `internal/application/admin`, `internal/application/provisioning`, `internal/auth`, `internal/infrastructure/postgres/admin_*`, `internal/infrastructure/postgres/api_key_provisioning_*`, `internal/transport/http/admin`, `internal/transport/http/provisioning` | `internal/app/admin_adapters.go`, `internal/app/provisioning.go`, `internal/app/transport.go`, `internal/worker/provisioningexpiration` | `internal/application/admin/*_test.go`, `internal/application/provisioning/*_test.go`, `internal/auth/*_test.go`, `internal/infrastructure/postgres/admin_*_test.go`, `internal/infrastructure/postgres/api_key_provisioning_*_test.go`, `internal/transport/http/admin/*_test.go`, `internal/transport/http/provisioning/*_test.go`, `internal/app/provisioning_*_test.go` | `go test ./internal/application/admin ./internal/application/provisioning ./internal/auth ./internal/infrastructure/postgres ./internal/transport/http/admin ./internal/transport/http/provisioning ./internal/app` | `verified` |
| Telegram alert base vertical slice | `docs/spec/030-routing-and-resellers.ru.md`, `docs/spec/060-admin-api.ru.md`, `docs/spec/070-database-schema.ru.md`, `docs/spec/090-configuration.ru.md` | `internal/application/telegramalert`, `internal/infrastructure/telegram/httpclient`, `internal/infrastructure/postgres/telegram_alert_store.go`, `internal/infrastructure/postgres/telegram_delivery_attempt_store.go`, `internal/worker/telegramdelivery`, `internal/worker/telegramfailedretry`, `internal/worker/telegrambalancescan`, `internal/worker/telegramstaleattemptrecovery` | `internal/app/telegram.go`, `internal/app/telegram_*_observer.go`, `internal/app/application.go`, `internal/app/worker.go` | `internal/application/telegramalert/*_test.go`, `internal/infrastructure/telegram/httpclient/*_test.go`, `internal/infrastructure/postgres/telegram_*_test.go`, `internal/app/telegram_*_test.go`, `internal/worker/telegram*/*_test.go`, `internal/app/admin_reseller_alert_repository_test.go` | `go test ./internal/application/telegramalert ./internal/infrastructure/telegram/... ./internal/infrastructure/postgres ./internal/app ./internal/worker/telegram...` | `verified` |
| Telegram admin listing and manual retry completion | `docs/spec/060-admin-api.ru.md` | `internal/application/admin/telegram_alerts.go`, `internal/transport/http/admin/telegram_alerts.go`, `internal/infrastructure/postgres/telegram_alert_store.go` | `internal/transport/http/admin/router.go`, `internal/app/admin_adapters.go`, `internal/app/application.go` | `internal/application/admin/telegram_alerts_test.go`, `internal/transport/http/admin/router_test.go`, `internal/transport/http/admin/stage10_major_contract_test.go`, `internal/transport/http/admin/stage10_v5_stage10_major_contract_test.go`, `internal/app/admin_reseller_alert_repository_test.go` | `go test ./internal/application/admin ./internal/transport/http/admin ./internal/app` | `verified` |
| Native API families | `docs/spec/011-native-api-families.ru.md`, `docs/adr/0002-native-api-auth-carriers.md`, `docs/spec/020-auth-and-billing-identity.ru.md` | `internal/transport/http/nativeapi`, `internal/transport/http/publicapi`, `internal/transport/httptransport`, `internal/infrastructure/requestmeta/openaicompat`, `internal/infrastructure/forwarding/anthropicnative`, `internal/infrastructure/forwarding/gemininative`, `internal/infrastructure/forwarding/ollamanative` | `internal/app/transport.go`, `internal/app/forwarding.go`, `internal/app/application.go` | `internal/transport/http/nativeapi/family_test.go`, `internal/transport/http/publicapi/llm_test.go`, `internal/transport/http/publicapi/router_test.go`, `internal/transport/httptransport/router_test.go`, `internal/app/public_llm_http_composition_test.go`, `internal/app/forwarding_test.go`, `internal/infrastructure/requestmeta/openaicompat/adapter_test.go`, `internal/infrastructure/forwarding/anthropicnative/*_test.go`, `internal/infrastructure/forwarding/gemininative/*_test.go`, `internal/infrastructure/forwarding/ollamanative/*_test.go`, `internal/application/llmrequest/service_test.go` | `go test ./internal/transport/http/... ./internal/infrastructure/requestmeta/... ./internal/infrastructure/forwarding/... ./internal/application/llmrequest ./internal/app` | `verified` |
- [x] Anthropic native supports auth-carrier normalization, model extraction, forwarding, usage extraction, failure classification and transport-to-ledger evidence.
- [x] Gemini native supports path-model transport, model listing, forwarding, usage extraction, failure classification and transport-to-ledger evidence.
- [x] Ollama native supports chat/generate/embeddings/tags transport, body-model extraction, forwarding, usage extraction, failure classification and transport-to-ledger evidence.
- [x] Native forwarding strips inbound Tokenio credentials and uses provider credentials before upstream dispatch.
| Configuration, logging and security audit | `docs/spec/090-configuration.ru.md`, `docs/spec/020-auth-and-billing-identity.ru.md`, `docs/spec/080-error-model.ru.md` | `internal/config`, `internal/auth`, `internal/infrastructure/secrets/envresolver`, `internal/transport/httptransport`, plus current stdlib logging in `cmd/*` and `internal/app/*` | `internal/app/runtime.go`, `internal/app/security.go`, `internal/app/transport.go`, `internal/app/worker.go` | Existing: `internal/config/*_test.go`, `internal/config/config_audit_test.go`, `internal/auth/*_test.go`, `internal/infrastructure/secrets/envresolver/*_test.go`, `internal/transport/httptransport/*_test.go`; config audit now verifies documented env keys are consumed and flags implementation-only env keys through an explicit pending-spec allowlist; missing central structured logger/redaction evidence | `go test ./internal/config ./internal/auth ./internal/infrastructure/secrets/... ./internal/transport/httptransport ./internal/app` | `pending` |
- [x] Configuration spec documents worker cadence, stale-attempt recovery and HTTP shutdown env keys consumed by `internal/config`.
- [x] Config parsing/validation evidence maps every consumed env key to a typed field, parser, validation rule and behavioral coverage.
- [x] Config composition-root consumer evidence maps non-logging typed config fields to `internal/app` consumers.
- [x] Composition-root consumer evidence maps parsed config fields to `internal/app` wiring.
- [x] Central structured logger/redactor exists and consumes `LogLevel`, `LogFormat` and `LogBodies` through `internal/app` logging composition.
- [x] Current stdlib logging sites are audited and frozen pending central structured logger/redactor implementation.
- [x] Worker observer factories use the central logging graph stdlib bridge instead of direct `log.Default()` calls.
- [x] `cmd/gateway`, `cmd/migrate` and gateway startup logs route process errors and listen events through the central logger/redactor.
| Reproducible integration environment | All runtime specs | Required `integration/` package with fake Postgres/Billing/OpenAI/Anthropic/Gemini/Ollama/Telegram environment | Required gateway/migration startup orchestration for integration tests | Required `integration/...` tests for public APIs, admin/provisioning, workers, restart and failure scenarios | `go test -tags=integration ./integration/...` | `pending` |
| Production verification | All specs and ADRs | Full repository after all implementation stages | Gateway/migrate binaries and runtime lifecycle | Clean checkout verification evidence, race tests, integration tests, shutdown/restart/concurrency checks | `gofmt -w . && go vet ./... && go test ./... && go test -race ./... && go test -tags=integration ./integration/... && go build ./cmd/gateway && go build ./cmd/migrate` | `pending` |

## Remaining implementation order

1. Add shared native-family transport/auth contract.
2. Implement Anthropic native vertical slice.
3. Implement Gemini native vertical slice.
4. Implement Ollama native vertical slice.
5. Add cross-family routing and credential stripping acceptance tests.
6. Complete config/logging/security audit.
7. Add reproducible integration environment.
8. Run final production verification and update this matrix with final command evidence.

## Security verification

- [x] Security audit verifies reseller credential access stays behind configured secret resolver wiring.

- [x] Security audit verifies user API key hashing uses HMAC-SHA256 and rejects non-HMAC/raw hash alternatives in API-key-hash production code.
- [x] Security audit verifies raw secrets are absent from admin audit state and raw-secret database persistence paths.
- [x] Security audit verifies query-string credentials are rejected before authentication for model catalog and LLM credential extraction.
- [x] Security audit verifies hop-by-hop upstream response headers are stripped before client response pass-through.
- [x] Security audit verifies inbound Tokenio authorization/API-key headers stop before forwarding stage.
- [x] Security audit verifies admin auth accepts only the configured admin token and rejects public API keys before admin service dispatch.
- [x] Security audit verifies provisioning auth is based on `X-Service-Token` and is separate from public/admin `Authorization` bearer credentials.
- [x] Billing JWT identity tokens carry fixed issuer/audience claims, enforce configured TTL as `exp-iat`, and reject missing signing key, TTL or clock configuration.
- [x] Startup validation fails before runtime construction when `TOKENIO_API_KEY_HASH_SECRET` is empty or whitespace-only.

- [x] Integration environment provides one Docker Compose stack for Postgres and the app.
- [x] Integration environment documents explicit Docker Compose migration execution before app startup.

- [x] Integration environment includes a Docker Compose smoke script that starts the app with documented environment and checks `/readyz`.
- [x] Integration smoke path checks `/readyz` and verifies `/v1/models` is reachable and protected.
- [x] Integration environment documents and scripts local Docker Compose cleanup with volume/orphan removal.

- [x] CI runs Go unit tests with `go test ./...` on pull requests and pushes to `main`.
- [x] CI runs `go vet ./...` after unit tests.
- [x] CI merge policy documents required branch protection check `Unit tests` for blocking failed checks.
- [x] CI required local checks are documented and scripted with `scripts/check.sh`.

- [x] Integration test package exists with build tag `integration` and a baseline smoke test.
- [x] Integration PostgreSQL dependency is reproducible through Docker Compose scripts and verified by `integration/postgres_test.go`.
- [x] Integration fake Billing service exists at `integration/fakes/billing` with request recording and programmable responses.
- [x] Integration fake OpenAI-compatible upstream exists at `integration/fakes/openaicompat` with deterministic defaults and programmable responses.
- [x] Integration fake Anthropic upstream exists at `integration/fakes/anthropic` with deterministic `/v1/messages` defaults and programmable responses.
- [x] Integration fake Gemini upstream exists at `integration/fakes/gemini` with deterministic native endpoint defaults and programmable responses.
- [x] Integration fake Ollama upstream exists at `integration/fakes/ollama` with deterministic native endpoint defaults and programmable responses.
- [x] Integration fake Telegram API exists at `integration/fakes/telegram` with deterministic `sendMessage` default and programmable responses.
- [x] Integration migrations and gateway lifecycle command exists at `scripts/integration-lifecycle-smoke.sh`.
- [x] Integration external-service audit exists at `integration/no_external_services_test.go`.
- [x] Fake service success scenario exists at `integration/fake_success_test.go`.
- [x] Fake service invalid request scenario exists at `integration/fake_invalid_request_test.go`.
- [x] Fake service authentication failure scenario exists at `integration/fake_authentication_failure_test.go`.
- [x] Fake service rate-limit scenario exists at `integration/fake_rate_limit_test.go`.
- [x] Fake service quota exhausted scenario exists at `integration/fake_quota_exhausted_test.go`.
- [x] Fake service provider 5xx scenario exists at `integration/fake_provider_5xx_test.go`.
