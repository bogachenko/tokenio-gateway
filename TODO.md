# Tokenio Gateway — TODO

## Правила выполнения

* [ ] Не добавлять provider-specific условия в generic transport/application/runtime.
* [ ] Не добавлять fallback между разными `api_family`.
* [ ] Не изменять semantic request payload, кроме разрешённого model identifier rewrite.
* [ ] Не добавлять временные, legacy или MVP paths.
* [ ] Не считать функцию реализованной без wiring, persistence и автоматического acceptance test.
* [ ] Один этап должен завершаться рабочим вертикальным сценарием.
* [ ] После каждого этапа выполнять:

  ```bash
  gofmt -w .
  go vet ./...
  go test ./...
  go test -race ./...
  go build ./cmd/gateway
  go build ./cmd/migrate
  ```
* [ ] Статус `verified` в `docs/implementation-matrix.md` разрешён только при наличии конкретного test path и команды запуска.

---

# 1. Восстановить исполнимый baseline

## 1.1. SQL migrations

* [ ] Проверить наличие canonical migrations в `db/migrations/*.up.sql`.
* [ ] Если migrations отсутствуют, создать полный набор forward-only migrations.
* [ ] Реализовать все таблицы из `docs/spec/070-database-schema.ru.md`.
* [ ] Добавить foreign keys.
* [ ] Добавить unique constraints.
* [ ] Добавить check constraints для enum-like состояний.
* [ ] Добавить индексы для worker scans и runtime lookup.
* [ ] Добавить partial indexes для pending/failed записей.
* [ ] Добавить CAS-compatible поля и predicates.
* [ ] Добавить schema migration metadata.
* [ ] Добавить checksum validation.
* [ ] Запретить runtime filesystem override canonical migrations.
* [ ] Убедиться, что `cmd/gateway` не применяет migrations.
* [ ] Убедиться, что `cmd/migrate` применяет migrations под advisory lock.
* [ ] Убедиться, что gateway fail-fast завершается при:

  * [ ] отсутствующей schema;
  * [ ] schema behind;
  * [ ] несовместимой schema ahead;
  * [ ] checksum mismatch.

## 1.2. Migration integration tests

* [ ] Добавить PostgreSQL integration test harness.
* [ ] Проверить migrate на пустой БД.
* [ ] Проверить повторный migrate.
* [ ] Проверить последовательный upgrade.
* [ ] Проверить checksum mismatch.
* [ ] Проверить concurrent migrate.
* [ ] Проверить gateway startup без schema.
* [ ] Проверить gateway startup со старой schema.
* [ ] Проверить gateway startup с корректной schema.

## 1.3. Architecture enforcement

* [ ] Добавить автоматический architecture test для dependency rules.
* [ ] Запретить импорты между соседними `internal/application/*`.
* [ ] Запретить `application -> infrastructure`.
* [ ] Запретить `transport -> infrastructure`.
* [ ] Запретить `domain -> application|ports|infrastructure|transport`.
* [ ] Запретить `os.Getenv` вне `internal/config`, composition root и разрешённых secret resolvers.

## Критерий завершения

* [ ] Чистый checkout собирается.
* [ ] Migrations полностью воспроизводят production schema.
* [ ] Architecture violations выявляются тестом.

---

# 2. Закрыть нарушения OpenAI-compatible contract

## 2.1. Model catalog capabilities

* [ ] В `internal/application/modelcatalog` заменить union capabilities на conservative intersection capabilities всех доступных routes.
* [ ] Не учитывать недоступные routes.
* [ ] При отсутствии доступных routes возвращать:

  * [ ] `active=false`;
  * [ ] отсутствующий pricing;
  * [ ] все capabilities `false`.
* [ ] Добавить тесты для routes с разными capability combinations.
* [ ] Проверить, что catalog не обещает capability, отсутствующую хотя бы у одного доступного route.

## 2.2. Единая application error model

* [ ] Создать общий normalized application error contract:

  ```text
  code
  safe_message
  category
  retryability
  request_stage
  cause
  ```
* [ ] Application layer не должен содержать HTTP status codes.
* [ ] Infrastructure classifiers должны возвращать normalized failure categories.
* [ ] Transport должен быть единственным владельцем HTTP status mapping.
* [ ] Удалить дублирующий error mapping из public/admin/provisioning routers.
* [ ] Не возвращать пользователю raw repository, Billing или upstream errors.

## 2.3. Исправить HTTP mapping

* [ ] `billing_unavailable` до upstream forwarding возвращать `502`.
* [ ] Invalid client/upstream request возвращать `400`.
* [ ] `upstream_unavailable` возвращать `502`.
* [ ] `no_route_available` возвращать `503`.
* [ ] `pricing_unavailable` возвращать `503`.
* [ ] Capacity exhaustion без доступного fallback возвращать `503`.
* [ ] Неизвестная gateway-owned ошибка возвращает `500 internal_error`.
* [ ] Все gateway-owned errors содержат `request_id`.

## 2.4. Boundary acceptance tests

* [ ] Проверить byte-for-byte сохранение request body.
* [ ] Проверить изменение только model identifier.
* [ ] Проверить byte-for-byte сохранение successful response body.
* [ ] Проверить invalid UTF-8.
* [ ] Проверить duplicate JSON keys на любом уровне.
* [ ] Проверить nesting depth limit.
* [ ] Проверить trailing JSON value.
* [ ] Проверить top-level non-object.
* [ ] Проверить `stream=true`.
* [ ] Проверить missing/invalid model.
* [ ] Проверить body size limit.
* [ ] Проверить полный набор billing headers.
* [ ] Проверить `pricing_failed` после успешного upstream response.
* [ ] Проверить все idempotency states.
* [ ] Проверить отсутствие raw upstream error body в gateway-owned response.

## Критерий завершения

* [ ] Все OpenAI-compatible endpoints соответствуют `docs/spec/010-external-api.ru.md`.
* [ ] Все ошибки соответствуют `docs/spec/080-error-model.ru.md`.

---

# 3. Завершить operational routing

## 3.1. Typed runtime policy

* [ ] Создать immutable routing policy в application contract.
* [ ] Собирать policy только в `internal/app`.
* [ ] Подключить:

  ```text
  TOKENIO_UPSTREAM_TIMEOUT
  TOKENIO_UPSTREAM_MAX_ATTEMPTS
  TOKENIO_UPSTREAM_MAX_BACKOFF
  TOKENIO_RATE_LIMIT_MAX_WAIT
  TOKENIO_COOLDOWN_RATE_LIMIT
  TOKENIO_COOLDOWN_QUOTA_EXCEEDED
  TOKENIO_COOLDOWN_5XX
  TOKENIO_COOLDOWN_TIMEOUT
  TOKENIO_COOLDOWN_AUTH_ERROR
  ```
* [ ] Удалить config fields, которые не имеют runtime consumer.

## 3.2. Attempt execution

* [ ] Ограничить общее количество forwarding attempts.
* [ ] Создавать отдельный `context.WithTimeout` для каждого attempt.
* [ ] Реализовать bounded exponential backoff.
* [ ] Поддержать безопасный `Retry-After`.
* [ ] Не ждать дольше configured rate-limit maximum wait.
* [ ] Не повторять non-retryable request errors.
* [ ] Не повторять uncertain-processing requests, если повтор может создать duplicate provider operation.
* [ ] Всегда освобождать concurrency/rate capacity.
* [ ] Всегда завершать durable forwarding attempt state.

## 3.3. Failure classification

* [ ] Расширить adapter classifiers категориями:

  ```text
  request_error
  auth_error
  rate_limit
  quota_exceeded
  provider_5xx
  timeout
  connection_error
  uncertain_processing
  malformed_response
  ```
* [ ] Classification должна находиться в concrete provider adapter.
* [ ] Generic routing не должен определять provider behavior по имени provider.
* [ ] Не использовать substring/keyword heuristics вне adapter-owned protocol parsing.

## 3.4. Durable cooldown

* [x] Добавить application port для изменения operational route state.
* [x] Атомарно сохранять cooldown и route event.
* [x] Применять разные cooldown durations по normalized failure category.
* [x] Auth error должен отключать route на configured cooldown.
* [x] Quota exceeded должен отличаться от temporary rate limit.
* [x] Истёкший cooldown должен снова делать route доступным.
* [x] Добавить recovery/cleanup истёкших cooldown records при необходимости.

## 3.5. Route events

* [ ] Подключить существующий `route_events` repository к application layer.
* [ ] Сохранять события:

  ```text
  selected
  skipped
  capacity_rejected
  forwarding_started
  retry_scheduled
  forwarding_succeeded
  forwarding_failed
  cooldown_set
  cooldown_expired
  ```
* [ ] Не сохранять raw API keys, JWT, request body или provider secrets.
* [ ] Добавить deterministic reason codes.
* [ ] Добавить correlation через `local_request_id`, route, reseller и attempt.

## 3.6. Routing tests

* [ ] Самый дешёвый доступный route выбирается первым.
* [ ] Priority и route ID используются как deterministic tie-breaker.
* [ ] Fallback не пересекает `api_family`.
* [ ] Fallback не пересекает `endpoint_kind`.
* [ ] Fallback не пересекает `client_model`.
* [ ] Unsupported capability исключает route.
* [ ] Disabled route/reseller исключается.
* [ ] Cooldown route исключается.
* [ ] Недостаточный reseller balance исключает route.
* [ ] RPM/TPM/concurrency capacity корректно резервируется и освобождается.
* [ ] Max attempts соблюдается.
* [ ] Timeout и backoff соблюдаются.
* [ ] Route events соответствуют фактическому выполнению.

## Критерий завершения

* [ ] Routing полностью соответствует `docs/spec/030-routing-and-resellers.ru.md`.
* [ ] Все operational decisions имеют durable audit trail.

---

# 4. Завершить durable billing и auto-charge

## 4.1. Recovery prepared batches

* [ ] Восстанавливать `pending` batches.
* [ ] Восстанавливать retryable `failed` batches.
* [ ] Использовать тот же immutable billing charge request ID.
* [ ] Не создавать новый external charge для succeeded batch.
* [ ] Корректно обрабатывать неизвестный результат Billing.
* [ ] Ограничивать число batch operations за recovery cycle.

## 4.2. Создание новых charge batches worker-ом

* [ ] После recovery prepared batches загружать новые billable candidates.
* [ ] Загружать partially charged records, у которых остался billable amount.
* [ ] Не загружать records, уже полностью allocated в active batch.
* [ ] Разбивать records по:

  ```text
  user
  provider_type
  client_model
  currency
  ```
* [ ] Для каждой группы создавать отдельный immutable billing command.
* [ ] Обрабатывать все группы за один recovery run в пределах operation budget.
* [ ] Не завершать run после первой группы.
* [ ] Claim применять только к records, реально включённым в allocations.
* [ ] Records без allocation должны оставаться доступными.
* [ ] После partial charge record должен снова стать доступным для остатка.

## 4.3. Transaction boundaries

* [ ] Prepare batch и allocations выполнять одной транзакцией.
* [ ] External Billing вызывать только после commit.
* [ ] Success reconciliation выполнять одной транзакцией.
* [ ] Failure reconciliation выполнять одной транзакцией.
* [ ] Использовать CAS/state predicates для concurrent workers.
* [ ] Исключить двойное списание при crash/restart.

## 4.4. Request-triggered auto-charge

* [ ] Сохранить request-triggered auto-charge как ускорение.
* [ ] Не считать его единственным trigger.
* [ ] Использовать тот же durable command pipeline, что и recovery worker.
* [ ] Не иметь отдельной финансовой логики для request path.

## 4.5. Billing tests

* [ ] Crash до prepare.
* [ ] Crash после prepare.
* [ ] Crash после external Billing success.
* [ ] Crash до local success reconciliation.
* [ ] Несколько charge groups.
* [ ] Partial charge.
* [ ] Record без allocation.
* [ ] Concurrent workers.
* [ ] Failed batch retry.
* [ ] Succeeded batch replay.
* [ ] Restart без новых LLM requests.
* [ ] Unknown Billing result.
* [ ] Отсутствие duplicate charge.

## Критерий завершения

* [ ] Pending usage гарантированно списывается без новых пользовательских запросов.
* [ ] Один usage amount не может быть списан дважды.
* [ ] Полный workflow соответствует `docs/spec/050-ledger-and-auto-charge.ru.md`.

---

# 5. Завершить Admin API и provisioning

## 5.1. Недостающие Admin endpoints

* [ ] Добавить:

  ```text
  GET /admin/v1/api-key-provisionings/{id}
  GET /admin/v1/route-events
  ```
* [ ] После реализации Telegram добавить:

  ```text
  GET  /admin/v1/telegram-alerts
  POST /admin/v1/telegram-alerts/{id}/retry
  ```

## 5.2. Admin mutation invariants

* [ ] Каждая mutation должна выполняться одной транзакцией.
* [ ] Загружать current state до изменения.
* [ ] Проверять expected state.
* [ ] Сохранять точный `before_state`.
* [ ] Сохранять точный `after_state`.
* [ ] Сохранять обязательный reason.
* [ ] Сохранять admin identity.
* [ ] Не сохранять secrets в audit.
* [ ] Не завершать mutation без audit event.

## 5.3. Provisioning contract

* [ ] Проверить first provision.
* [ ] Проверить same-input idempotent replay.
* [ ] Проверить conflicting replay.
* [ ] Запретить provision для disabled user.
* [ ] Не создавать второй active API key при replay.
* [ ] Хранить raw material только encrypted.
* [ ] Не писать raw key в logs/audit/errors.
* [ ] Confirm delivery должен быть idempotent.
* [ ] Expiration worker должен закрывать недоставленные provisioning records.
* [ ] Delivered raw key нельзя получить повторно.
* [ ] Status endpoint не должен раскрывать raw material.
* [ ] Provisioning repository должен использовать deterministic state transitions.

## 5.4. Admin acceptance tests

* [ ] CRUD users.
* [ ] CRUD API keys.
* [ ] CRUD resellers.
* [ ] CRUD routes.
* [ ] CRUD route prices.
* [ ] Reseller balance correction.
* [ ] Usage record listing.
* [ ] Manual usage resolution.
* [ ] Charge batch retry.
* [ ] Route events listing.
* [ ] Provisioning lifecycle.
* [ ] Audit completeness.
* [ ] Authentication and authorization failures.

## Критерий завершения

* [ ] Admin API соответствует `docs/spec/060-admin-api.ru.md`.
* [ ] Provisioning соответствует `docs/spec/021-api-key-provisioning.ru.md`.

---

# 6. Завершить Telegram vertical slice

## 6.1. Composition root

* [ ] Создать `TelegramInfrastructureGraph`.
* [ ] Создать Telegram HTTP client только при валидной конфигурации.
* [ ] Fail-fast при частично заданной Telegram конфигурации.
* [ ] Подключить Telegram application services.
* [ ] Подключить Telegram repositories.
* [ ] Подключить Telegram workers в `WorkerGraph`.

## 6.2. Alert creation

* [ ] После commit изменения reseller balance выполнять best-effort threshold check.
* [ ] Не выполнять Telegram HTTP request внутри финансовой транзакции.
* [ ] Создавать durable pending alert.
* [ ] Использовать deterministic deduplication key.
* [ ] Не создавать повторный active alert для того же threshold condition.
* [ ] Поддержать восстановление после пополнения баланса и повторное срабатывание.

## 6.3. Delivery workers

* [ ] Реализовать pending delivery worker.
* [ ] Реализовать retry failed delivery worker.
* [ ] Подключить stale-attempt recovery worker.
* [ ] Добавить attempt timeout.
* [ ] Добавить retry/backoff policy.
* [ ] Сохранять Telegram message ID при успехе.
* [ ] Ошибка Telegram не должна откатывать финансовую операцию.

## 6.4. Periodic balance scan

* [ ] Добавить periodic scan всех enabled resellers.
* [ ] Проверять threshold независимо от новых LLM requests.
* [ ] Использовать те же application rules, что и post-commit check.
* [ ] Ограничить batch size и operation budget.

## 6.5. Admin operations

* [ ] Реализовать listing Telegram alerts.
* [ ] Реализовать manual retry.
* [ ] Manual retry должен создавать admin audit event.
* [ ] Нельзя повторно отправлять уже успешно доставленный alert без explicit admin transition.

## 6.6. Telegram tests

* [ ] Threshold crossing.
* [ ] Repeated balance update без duplicate alert.
* [ ] Temporary Telegram failure.
* [ ] Permanent Telegram failure.
* [ ] Stale attempt recovery.
* [ ] Restart recovery.
* [ ] Manual retry.
* [ ] Ошибка Telegram не влияет на committed balance operation.

## Критерий завершения

* [ ] Telegram alerts работают после restart и без новых запросов.
* [ ] Доставка полностью durable и наблюдаема через Admin API.

---

# 7. Реализовать native API families

## 7.1. Общий registry contract

* [ ] Registry должен выбирать adapter по:

  ```text
  api_family + provider_type
  ```
* [ ] Отдельно регистрировать:

  ```text
  inbound parser
  auth carrier parser
  forwarding adapter
  model rewrite capability
  usage extractor
  failure classifier
  header policy
  model catalog adapter
  ```
* [ ] Generic application pipeline не должен импортировать concrete adapters.
* [ ] Unknown `api_family + provider_type` должен deterministic отклоняться.
* [ ] Native implementation не должна изменять OpenAI-compatible contract.

## 7.2. Anthropic native

* [ ] Реализовать `POST /v1/messages`.
* [ ] Принимать Tokenio key только через `x-api-key`.
* [ ] Reject conflicting/unsupported auth carriers.
* [ ] Не передавать Tokenio key upstream.
* [ ] Извлекать model из native request.
* [ ] Сохранять body byte-for-byte.
* [ ] Поддержать model-only rewrite.
* [ ] Реализовать Anthropic usage extraction.
* [ ] Реализовать Anthropic failure classifier.
* [ ] Возвращать upstream response body без изменений.
* [ ] Добавить native acceptance tests.

## 7.3. Gemini native

* [ ] Реализовать:

  ```text
  POST /v1beta/models/{model}:generateContent
  POST /v1beta/models/{model}:embedContent
  POST /v1beta/models/{model}:batchEmbedContents
  GET  /v1beta/models
  ```
* [ ] Принимать Tokenio key только через `x-goog-api-key`.
* [ ] Запретить query-string `?key=`.
* [ ] Извлекать model из path.
* [ ] Поддержать model path-segment rewrite.
* [ ] Сохранять body byte-for-byte.
* [ ] Реализовать Gemini usage extraction.
* [ ] Реализовать Gemini failure classifier.
* [ ] Возвращать upstream response body без изменений.
* [ ] Добавить native acceptance tests.

## 7.4. Ollama native

* [ ] Реализовать:

  ```text
  POST /api/chat
  POST /api/generate
  POST /api/embeddings
  GET  /api/tags
  ```
* [ ] Принимать Tokenio key через `Authorization: Bearer`.
* [ ] Извлекать model из native request.
* [ ] Сохранять body byte-for-byte.
* [ ] Поддержать model-only rewrite.
* [ ] Реализовать Ollama usage extraction или conservative estimation.
* [ ] Реализовать Ollama failure classifier.
* [ ] Возвращать upstream response body без изменений.
* [ ] Добавить native acceptance tests.

## 7.5. Cross-family tests

* [ ] OpenAI request не может попасть в Anthropic/Gemini/Ollama native route.
* [ ] Anthropic request не может попасть в другую family.
* [ ] Gemini request не может попасть в другую family.
* [ ] Ollama request не может попасть в другую family.
* [ ] Model rewrite не разрешает API conversion.
* [ ] Tokenio credential никогда не передаётся upstream.
* [ ] Billing model отражает фактически использованный provider type и client model.

## Критерий завершения

* [ ] Все native paths соответствуют `docs/spec/011-native-api-families.ru.md`.
* [ ] Для каждой family реализован полный transport-to-ledger vertical slice.

---

# 8. Завершить configuration, logging и security

## 8.1. Config consumption audit

Для каждого config field обеспечить:

* [ ] env key;

* [ ] parsing;

* [ ] validation;

* [ ] typed config field;

* [ ] composition-root consumer;

* [ ] behavioral test;

* [ ] documentation.

* [ ] Удалить неиспользуемые config fields.

* [ ] Не читать env внутри application/domain/transport.

## 8.2. Logging

* [ ] Создать central structured logger.
* [ ] Подключить log level.
* [ ] Подключить log format.
* [ ] Добавить correlation fields:

  ```text
  local_request_id
  user_id
  api_key_id
  route_id
  reseller_id
  forwarding_attempt_id
  billing_batch_id
  ```
* [ ] Не логировать request/response bodies по умолчанию.
* [ ] `LogBodies=true` в production должен останавливать startup.
* [ ] Добавить redaction для:

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

## 8.3. Security verification

* [ ] User API keys хешируются только HMAC-SHA256.
* [ ] Startup завершается без `TOKENIO_API_KEY_HASH_SECRET`.
* [ ] Billing JWT имеет корректные issuer/audience/TTL.
* [ ] Reseller keys разрешаются только через configured secret resolver.
* [ ] Raw secrets отсутствуют в database, audit, errors и logs.
* [ ] Query-string credentials запрещены.
* [ ] Hop-by-hop headers удаляются.
* [ ] Inbound Tokenio auth headers удаляются перед forwarding.
* [ ] Admin auth отделён от public auth.
* [ ] Provisioning auth отделён от public/admin auth.

## Критерий завершения

* [ ] Все config keys из `docs/spec/090-configuration.ru.md` имеют реального consumer.
* [ ] Security invariants покрыты автоматическими тестами.

---

# 9. Integration environment

## 9.1. Test dependencies

* [ ] Создать `integration/`.
* [ ] Добавить reproducible PostgreSQL environment.
* [ ] Добавить fake Billing service.
* [ ] Добавить fake OpenAI-compatible upstream.
* [ ] Добавить fake Anthropic upstream.
* [ ] Добавить fake Gemini upstream.
* [ ] Добавить fake Ollama upstream.
* [ ] Добавить fake Telegram API.
* [ ] Добавить команду запуска migrations.
* [ ] Добавить команду запуска gateway.
* [ ] Не использовать внешние production services в integration tests.

## 9.2. Управляемые сценарии fake services

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

## 9.3. End-to-end scenarios

* [ ] Public authentication.
* [ ] Model catalog.
* [ ] Chat completion.
* [ ] Embeddings.
* [ ] Image generation.
* [ ] Anthropic messages.
* [ ] Gemini generate/embed/models.
* [ ] Ollama chat/generate/embeddings/tags.
* [ ] Route fallback.
* [ ] Cooldown.
* [ ] Capacity rejection.
* [ ] Usage finalization.
* [ ] Request-triggered charge.
* [ ] Worker-triggered charge.
* [ ] Restart recovery.
* [ ] Provisioning lifecycle.
* [ ] Admin mutations and audit.
* [ ] Telegram alert lifecycle.
* [ ] Migration lifecycle.

## Критерий завершения

* [ ] Полный сервис поднимается одной воспроизводимой командой.
* [ ] Все внешние зависимости заменяемы deterministic fake implementations.

---

# 10. Production verification

* [ ] Выполнить на clean checkout:

  ```bash
  gofmt -w .
  go vet ./...
  go test ./...
  go test -race ./...
  go test -tags=integration ./integration/...
  go build ./cmd/gateway
  go build ./cmd/migrate
  ```
* [ ] Проверить shutdown по `SIGTERM`.
* [ ] Проверить остановку всех workers.
* [ ] Проверить закрытие HTTP server.
* [ ] Проверить закрытие PostgreSQL pool.
* [ ] Проверить отсутствие goroutine leaks.
* [ ] Проверить bounded worker cycles.
* [ ] Проверить restart во всех durable intermediate states.
* [ ] Проверить concurrent gateway replicas.
* [ ] Проверить concurrent workers.
* [ ] Проверить migration deployment order.
* [ ] Проверить отсутствие schema mutation из gateway process.
* [ ] Проверить логи на отсутствие secrets.
* [ ] Проверить все public/admin/internal error envelopes.
* [ ] Проверить все response headers.
* [ ] Проверить все API families реальными SDK-compatible requests.

---

# 11. Финальная актуализация документации

* [ ] Обновить `docs/implementation-matrix.md`.
* [ ] Для каждого требования указать:

  ```text
  specification section
  implementation path
  wiring path
  test path
  verification command
  status
  ```
* [ ] Удалить устаревшие и дублирующие пункты.
* [ ] Не оставлять `verified` без acceptance evidence.
* [ ] Проверить соответствие:

  ```text
  docs/spec/000-tokenio-gateway.ru.md
  docs/spec/010-external-api.ru.md
  docs/spec/011-native-api-families.ru.md
  docs/spec/020-auth-and-billing-identity.ru.md
  docs/spec/021-api-key-provisioning.ru.md
  docs/spec/030-routing-and-resellers.ru.md
  docs/spec/040-pricing-and-usage.ru.md
  docs/spec/050-ledger-and-auto-charge.ru.md
  docs/spec/060-admin-api.ru.md
  docs/spec/070-database-schema.ru.md
  docs/spec/080-error-model.ru.md
  docs/spec/090-configuration.ru.md
  docs/adr/*
  ```