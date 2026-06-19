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

## 2. Implement native API families

Specification source: `docs/spec/011-native-api-families.ru.md`, `docs/adr/0002-native-api-auth-carriers.md`.

Native support must be implemented as parallel vertical slices. Do not convert native request bodies into OpenAI-compatible request bodies.

### 2.1. Shared native-family contract

* [x] Add transport-level family/auth extraction contract for native adapters.
* [x] Normalize each accepted auth carrier into the same internal Tokenio raw API key contract.
* [x] Reject conflicting/unsupported carriers deterministically.
* [ ] Ensure inbound Tokenio credentials are stripped before upstream forwarding.
* [ ] Ensure route selection is always constrained by `api_family + endpoint_kind + client_model`.
* [ ] Ensure fallback never crosses `api_family`.
* [ ] Add cross-family acceptance tests.

### 2.2. Anthropic native

* [x] Implement `POST /v1/messages`.
* [x] Accept Tokenio key only through `x-api-key`.
* [x] Extract model from native request.
* [x] Preserve body byte-for-byte except allowed model identifier rewrite.
* [x] Implement Anthropic forwarding adapter.
* [x] Wire Anthropic forwarding factory into application forwarding registry.
* [x] Implement Anthropic usage extraction.
* [x] Implement Anthropic failure classifier.
* [x] Return upstream success body byte-for-byte.
* [x] Add transport-to-ledger acceptance tests.

### 2.3. Gemini native

* [ ] Implement:

  ```text
  POST /v1beta/models/{model}:generateContent
  POST /v1beta/models/{model}:embedContent
  POST /v1beta/models/{model}:batchEmbedContents
  GET  /v1beta/models
  ```

* [x] Accept Tokenio key only through `x-goog-api-key`.
* [x] Reject query-string `?key=` credentials.
* [x] Extract model from path.
* [x] Preserve body byte-for-byte except allowed path model segment rewrite.
* [x] Implement Gemini forwarding adapter.
* [x] Wire Gemini forwarding factory into application forwarding registry.
* [x] Implement Gemini usage extraction.
* [x] Implement Gemini failure classifier.
* [x] Return upstream success body byte-for-byte.
* [x] Add transport-to-ledger acceptance tests.

### 2.4. Ollama native

* [x] Implement public transport dispatch for:

  ```text
  POST /api/chat
  POST /api/generate
  POST /api/embeddings
  ```

* [x] Implement public model listing transport:

  ```text
  GET  /api/tags
  ```

* [x] Accept Tokenio key through `Authorization: Bearer`.
* [x] Extract model from native request.
* [x] Preserve body byte-for-byte except allowed model identifier rewrite.
* [x] Implement Ollama forwarding adapter.
* [ ] Implement Ollama usage extraction or conservative usage estimation.
* [ ] Implement Ollama failure classifier.
* [x] Return upstream success body byte-for-byte.
* [x] Add transport-to-ledger acceptance tests.

### Verification command

```bash
gofmt -w .
go test ./internal/transport/http/... ./internal/infrastructure/requestmeta/... ./internal/infrastructure/forwarding/... ./internal/application/llmrequest ./internal/app
```

---

## 3. Finish configuration, logging and security audit

### 3.1. Config consumption audit

For every key in `docs/spec/090-configuration.ru.md` verify:

* [ ] env key exists;
* [ ] parsing exists;
* [ ] validation exists;
* [ ] typed config field exists;
* [ ] composition-root consumer exists;
* [ ] behavioral test exists;
* [ ] documentation is current.

Remove unused config fields instead of keeping dead knobs.

### 3.2. Structured logging and redaction

* [ ] Add central structured logger.
* [ ] Wire log level and log format from config.
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

* [ ] Do not log request/response bodies by default.
* [ ] Fail startup when body logging is enabled in production.
* [ ] Redact:

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

* [ ] User API keys are hashed only with HMAC-SHA256.
* [ ] Startup fails without `TOKENIO_API_KEY_HASH_SECRET`.
* [ ] Billing JWT issuer/audience/TTL are validated.
* [ ] Reseller keys resolve only through configured secret resolver.
* [ ] Raw secrets are absent from database, audit, errors and logs.
* [ ] Query-string credentials are rejected.
* [ ] Hop-by-hop headers are removed.
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
