# Tokenio Gateway — implementation TODO

Статус документа: reconciled against `docs/spec/*`, `docs/adr/*`, package tree and wiring evidence.

Этот файл больше не является историческим чеклистом всех уже закрытых этапов. Он фиксирует только оставшуюся работу до production-ready сервиса. Подробная сверка `spec -> implementation -> wiring -> tests` находится в `docs/implementation-matrix.md`.

## Правила выполнения

* [ ] Не добавлять provider-specific условия в generic transport/application/runtime.
* [ ] Не добавлять fallback между разными `api_family`.
* [ ] Не изменять semantic request payload, кроме разрешённого model identifier rewrite.
* [ ] Не добавлять временные, legacy или MVP paths.
* [ ] Не считать функцию реализованной без wiring, persistence и automated acceptance evidence.
* [ ] Один этап должен завершаться рабочим vertical slice.
* [ ] После каждого implementation этапа выполнять релевантные проверки и обновлять `docs/implementation-matrix.md` точными code/test paths.

---

## 0. Уже закрытые baseline-вертикали

Эти блоки не нужно реализовывать повторно. Они считаются закрытыми на уровне пакетов, wiring и automated tests, если не будет найден новый regression.

* [x] SQL migrations и schema contracts.
* [x] `cmd/migrate` применяет migrations отдельно от gateway runtime.
* [x] `cmd/gateway` запускает gateway runtime без автоматического применения migrations.
* [x] Architecture dependency rules.
* [x] OpenAI-compatible public API:

  ```text
  GET  /v1/models
  POST /v1/chat/completions
  POST /v1/embeddings
  POST /v1/images/generations
  ```

* [x] OpenAI-compatible request parsing, body preservation, model-only rewrite, response passthrough and error mapping acceptance coverage.
* [x] Model catalog conservative capabilities and public pricing.
* [x] Operational routing, route capacity, retry/fallback, cooldown and route events.
* [x] Durable usage ledger and billing auto-charge.
* [x] Billing recovery workers and request-triggered auto-charge.
* [x] Admin API core endpoints for users, API keys, resellers, routes, route prices, usage, charge batches, route events and provisioning records.
* [x] API key provisioning lifecycle.
* [x] Telegram infrastructure/application/worker base wiring.

Do not reopen these sections in this TODO unless a concrete failing test or missing spec requirement is identified.

---

## 1. Finish Telegram admin/retry completion

### 1.1. Admin transport endpoints

* [x] Expose `GET /admin/v1/telegram-alerts` in `internal/transport/http/admin`.
* [x] Expose `POST /admin/v1/telegram-alerts/{id}/retry` in `internal/transport/http/admin`.
* [x] Route both endpoints through `internal/application/admin`, not directly to Telegram application or Postgres.
* [x] Preserve admin auth, request ID and common response/error envelope behavior.

### 1.2. Admin application contract

* [x] Listing supports deterministic filters/pagination required by `docs/spec/060-admin-api.ru.md`.
* [x] Manual retry is allowed only for retryable alert states.
* [x] Manual retry records admin audit with reason and admin identity.
* [x] Successfully delivered alert cannot be sent again without explicit admin state transition.

### 1.3. Telegram delivery evidence gaps

* [x] Store Telegram message ID on successful delivery if Telegram API returns it.
* [x] Verify temporary Telegram failure lifecycle.
* [x] Verify permanent Telegram failure lifecycle.
* [x] Verify stale attempt recovery after restart.
* [x] Verify Telegram error never rolls back committed reseller balance operation.

### Verification command

```bash
gofmt -w .
go test ./internal/application/admin ./internal/application/telegramalert ./internal/transport/http/admin ./internal/app ./internal/worker/telegram...
go test ./internal/infrastructure/postgres -run 'Telegram'
```

---

## 2. Native API families — completed vertical slices

Specification source: `docs/spec/011-native-api-families.ru.md`, `docs/adr/0002-native-api-auth-carriers.md`.

Native support is implemented as parallel vertical slices. Native request bodies are not converted into OpenAI-compatible request bodies. Do not reopen this section unless a concrete failing test or missing spec requirement is identified.

### 2.1. Shared native-family contract

* [x] Transport-level family/auth extraction contract exists for native adapters.
* [x] Each accepted auth carrier is normalized into the internal Tokenio raw API key contract.
* [x] Conflicting/unsupported carriers are rejected deterministically.
* [x] Inbound Tokenio credentials are stripped before upstream forwarding.
* [x] Route selection is constrained by `api_family + endpoint_kind + client_model`.
* [x] Fallback does not cross `api_family`.
* [x] Cross-family acceptance evidence exists in native transport, forwarding registry and request lifecycle tests.

### 2.2. Anthropic native

* [x] `POST /v1/messages` public transport dispatch.
* [x] Tokenio key carrier: `x-api-key`.
* [x] Native model extraction from request body.
* [x] Body preservation except allowed model identifier rewrite.
* [x] Native forwarding adapter and application factory wiring.
* [x] Native usage extraction.
* [x] Native failure classifier.
* [x] Upstream success body passthrough.
* [x] Transport-to-ledger acceptance evidence.

### 2.3. Gemini native

* [x] Public transport dispatch for:

  ```text
  POST /v1beta/models/{model}:generateContent
  POST /v1beta/models/{model}:embedContent
  POST /v1beta/models/{model}:batchEmbedContents
  GET  /v1beta/models
  ```

* [x] Tokenio key carrier: `x-goog-api-key`.
* [x] Query-string `?key=` credentials are rejected.
* [x] Native model extraction from path.
* [x] Body preservation except allowed path model segment rewrite.
* [x] Native forwarding adapter and application factory wiring.
* [x] Native usage extraction.
* [x] Native failure classifier.
* [x] Upstream success body passthrough.
* [x] Transport-to-ledger acceptance evidence.

### 2.4. Ollama native

* [x] Public transport dispatch for:

  ```text
  POST /api/chat
  POST /api/generate
  POST /api/embeddings
  GET  /api/tags
  ```

* [x] Tokenio key carrier: `Authorization: Bearer`.
* [x] Native model extraction from request body.
* [x] Body preservation except allowed model identifier rewrite.
* [x] Native forwarding adapter and application factory wiring.
* [x] Native usage extraction from `prompt_eval_count` and `eval_count`.
* [x] Native failure classifier.
* [x] Upstream success body passthrough.
* [x] Transport-to-ledger acceptance evidence.

### Verification command

```bash
gofmt -w .
go test ./internal/transport/http/... ./internal/infrastructure/requestmeta/... ./internal/infrastructure/forwarding/... ./internal/application/llmrequest ./internal/app
```

---

## 3. Finish configuration, logging and security audit

### 3.1. Config consumption audit

* [x] Verify composition-root consumers for parsed config fields.

* [x] Reconcile implementation-only worker and HTTP shutdown env keys into `docs/spec/090-configuration.ru.md`.
For every key in `docs/spec/090-configuration.ru.md` verify:

* [x] Add automated audit that every documented `TOKENIO_*` key is consumed by `internal/config`.
* [x] Add automated audit that every consumed `TOKENIO_*` key is either documented or explicitly listed as pending spec reconciliation.
* [x] Reconcile implementation-only worker/shutdown env keys with `docs/spec/090-configuration.ru.md`.
* [x] Verify parsing exists for every documented key.
* [x] Verify validation exists for every documented key.
* [x] Verify typed config field exists for every documented key.
* [x] Verify composition-root consumer exists for every documented key.
* [ ] Add behavioral tests for missing/invalid/default values where coverage is still absent.
* [ ] Remove unused config fields instead of keeping dead knobs.

### 3.2. Structured logging and redaction

* [x] Audit current stdlib logging sites in `cmd/*` and `internal/app/*` and keep `LogLevel`/`LogFormat`/`LogBodies` scoped to structured logger implementation.
* [x] Add central structured logger.
* [x] Wire log level and log format from config.
* [x] Route gateway and migration process startup errors through the central logger/redactor.
* [x] Wire worker observer factories through the central logging graph stdlib bridge.
* [ ] Add correlation fields where available:

  ```text
  local_request_id
  user_id
  api_key_id
  route_id
  reseller_id
  forwarding_attempt_id
  billing_batch_id
  ```

* [x] Do not log request/response bodies by default.
* [x] Fail startup when body logging is enabled in production.
* [x] Wire central logger/redactor into existing stdlib logging callsites.
* [x] Redact:

  ```text
  Authorization
  x-api-key
  x-goog-api-key
  API keys
  JWT
  DSN password
  reseller credentials
  provisioning material
  Telegram bot token
  ```

### 3.3. Security verification

* [x] User API keys are hashed only with HMAC-SHA256.
* [x] Add automated security audit that user API key hash implementation imports `crypto/hmac` and `crypto/sha256` and rejects raw SHA256/MD5/SHA1/bcrypt/scrypt/argon2 in that context.
* [x] Startup fails without `TOKENIO_API_KEY_HASH_SECRET`.
* [x] Add behavioral startup evidence that empty or whitespace-only `TOKENIO_API_KEY_HASH_SECRET` fails before runtime construction.
* [x] Billing JWT issuer/audience/TTL are validated.
* [x] Reseller keys resolve only through configured secret resolver.
* [x] Add automated audit that reseller credential access stays behind configured secret resolver wiring.
* [x] Raw secrets are absent from database, audit, errors and logs.
* [x] Add automated audit that admin audit state rejects raw API keys, key hashes, encrypted raw keys, auth headers and service/admin tokens, and that DB/migration paths do not define raw-secret persistence columns.
* [x] Query-string credentials are rejected.
* [x] Add automated tests that model and LLM credential extraction reject `key`, `api_key`, `access_token`, auth-header aliases and provider API-key aliases in query strings.
* [x] Hop-by-hop headers are removed.
* [x] Add automated response-pass-through evidence that hop-by-hop upstream headers including `Connection`, `Keep-Alive`, `Proxy-*`, `TE`, `Trailer`, `Transfer-Encoding` and `Upgrade` are not copied to clients.
* [ ] Inbound Tokenio auth headers are removed before forwarding.
* [ ] Admin auth is separate from public auth.
* [ ] Provisioning auth is separate from public/admin auth.

### Verification command

```bash
gofmt -w .
go test ./internal/config ./internal/app ./internal/auth ./internal/infrastructure/secrets/... ./internal/transport/httptransport ./internal/transport/http/...
```

---

## 4. Add reproducible integration environment

### 4.1. Test dependencies

* [ ] Create `integration/`.
* [ ] Add reproducible PostgreSQL environment.
* [ ] Add fake Billing service.
* [ ] Add fake OpenAI-compatible upstream.
* [ ] Add fake Anthropic upstream.
* [ ] Add fake Gemini upstream.
* [ ] Add fake Ollama upstream.
* [ ] Add fake Telegram API.
* [ ] Add commands for migrations and gateway lifecycle.
* [ ] Do not use external production services in integration tests.

### 4.2. Fake service scenarios

* [ ] Success.
* [ ] Invalid request.
* [ ] Authentication failure.
* [ ] Rate limit.
* [ ] Quota exhausted.
* [ ] Provider 5xx.
* [ ] Timeout.
* [ ] Connection reset.
* [ ] Headers received, body failed.
* [ ] Missing usage.
* [ ] Malformed usage.
* [ ] Partial Billing charge.
* [ ] Duplicate Billing request.
* [ ] Unknown Billing result.
* [ ] Telegram temporary failure.
* [ ] Telegram permanent failure.

### 4.3. End-to-end scenarios

* [ ] Clean migration lifecycle.
* [ ] Public authentication.
* [ ] Model catalog.
* [ ] OpenAI chat completion.
* [ ] OpenAI embeddings.
* [ ] OpenAI image generation.
* [ ] Anthropic messages.
* [ ] Gemini generate/embed/models.
* [ ] Ollama chat/generate/embeddings/tags.
* [ ] Route fallback.
* [ ] Cooldown.
* [ ] Capacity rejection.
* [ ] Usage finalization.
* [ ] Request-triggered charge.
* [ ] Recovery worker charge.
* [ ] Gateway restart.
* [ ] Provisioning lifecycle.
* [ ] Admin mutation/audit.
* [ ] Telegram alert lifecycle.

### Verification command

```bash
go test -tags=integration ./integration/...
```

---

## 5. Production verification gate

Run on clean checkout after implementation stages are done:

```bash
gofmt -w .
go vet ./...
go test ./...
go test -race ./...
go test -tags=integration ./integration/...
go build ./cmd/gateway
go build ./cmd/migrate
```

Additional production checks:

* [ ] Gateway starts only against compatible schema.
* [ ] Gateway does not mutate schema at startup.
* [ ] SIGTERM closes HTTP server, workers and PostgreSQL pool.
* [ ] Worker cycles are bounded.
* [ ] No goroutine leaks in start/stop tests.
* [ ] Concurrent gateway replicas preserve idempotency and no duplicate external charges.
* [ ] Concurrent workers preserve durable command invariants.
* [ ] Logs do not contain secrets.
* [ ] All supported SDK-compatible requests pass for every API family.
* [ ] `docs/implementation-matrix.md` records exact final evidence.
