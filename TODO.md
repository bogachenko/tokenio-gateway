## Проблема в слое

Оставшаяся работа относится сразу к нескольким слоям:

* `domain/application` — финансовые и routing-инварианты;
* `ports/infrastructure` — Postgres, Billing, Telegram и provider adapters;
* `transport` — public/admin/native HTTP-контракты;
* `worker` — durable recovery;
* `app` — DI, конфигурация и lifecycle;
* `tests/integration` — доказательство соответствия спецификациям.

Источником истины должны быть `docs/spec/*` и `docs/adr/*`, а не текущий `TODO.md` или `README.md`. Это прямо закреплено в `AGENTS.md`. 

## Инвариант

На всех этапах необходимо сохранять:

```text
request semantic payload не конвертируется;
разрешён только explicit model identifier rewrite;
response body успешного upstream не изменяется;
fallback не пересекает api_family;
provider-specific логика остаётся в adapters;
финансовое состояние хранится в Postgres;
worker вызывает application service, а не repository напрямую;
внешнее списание невозможно без durable local ledger command.
```

Эти правила закреплены спецификацией и ADR.  

## Неправильное решение

Нельзя:

* добавлять все оставшиеся функции одним большим patch;
* сначала реализовывать native families поверх незакрытого billing recovery;
* подключать workers напрямую к Postgres;
* добавлять `if provider == ...` в generic routing или transport;
* считать наличие интерфейса доказательством выполнения спецификации;
* писать acceptance-тесты до устранения противоречий между спецификациями;
* сохранять текущие незадействованные config-поля как «реализованную конфигурацию».

## Правильное решение

Реализовывать по зависимостям: сначала нормализовать контракты и архитектурные границы, затем закрыть OpenAI-compatible core, durable finance и routing, после этого Telegram и native families, а в конце — единый integration environment и полная spec matrix.

---

# Пошаговый план

## Шаг 0. Зафиксировать baseline и устранить противоречия в спецификациях

### Сделать

1. Зафиксировать SHA актуального `main`.
2. Получить отдельный `git diff` локальной рабочей копии.
3. Не включать незакоммиченные изменения в статус GitHub `main`.
4. Создать `docs/implementation-matrix.md` с колонками:

```text
spec
section
requirement_id
requirement
implementation
automated_test
status
evidence
```

5. Разрешить нормативные противоречия.

### 0.1. Capabilities в `/v1/models`

`010` требует union capabilities доступных routes, а `030` — conservative intersection.  

Нужно принять одно решение и обновить обе спецификации. Практичный production-вариант первой версии:

```text
capabilities = intersection доступных routes
```

Это не обещает клиенту комбинацию возможностей, которую ни один route не может обслужить.

### 0.2. Auth для native SDK

`011` требует стандартные Anthropic/Gemini/Ollama paths и drop-in замену `base_url`, но `020` определяет единственный public auth contract:

```http
Authorization: Bearer sk_...
```

 

До native implementation нужен ADR:

```text
вариант A: все native endpoints принимают только Bearer sk_;
вариант B: family-native auth carriers принимают Tokenio sk_,
           но нормализуются transport adapter-ом в один public principal.
```

Для настоящей SDK drop-in совместимости нужен вариант B, однако конкретные разрешённые headers/query parameters должны быть явно описаны.

### 0.3. Billing recovery configuration

Добавить в `050` и `090` явный контракт:

```text
TOKENIO_BILLING_RECOVERY_INTERVAL
TOKENIO_BILLING_RECOVERY_BATCH_SIZE
```

Зафиксировать:

```text
первый цикл запускается сразу;
следующие — по interval;
один цикл ограничен batch size;
worker не зависит от нового LLM request.
```

### 0.4. Migration execution policy

`090` требует explicit command или controlled startup mode, а не неявные destructive migrations. 

Рекомендуемое решение:

```text
cmd/migrate     — применяет migrations;
cmd/gateway     — только проверяет совместимость schema version.
```

### Готово, когда

* противоречащих нормативных требований больше нет;
* каждый последующий PR ссылается на конкретные строки implementation matrix;
* локальный diff отделён от состояния `main`.

---

## Шаг 1. Устранить связи между application packages

### Проблема

`internal/application/billing` напрямую импортирует соседние application-пакеты `ledger` и `pricing`. 

Это нарушает установленное направление зависимостей:

```text
application -> domain + ports
```



### Сделать

1. Вынести чистые финансовые value objects и проверки в `internal/domain`.
2. Контракты, необходимые конкретному use case, определить на consumer side или в `internal/ports`.
3. Не превращать `application/ledger` и `application/pricing` в библиотеки общего назначения.
4. `internal/app` должен связать:

   * billing service;
   * pricing calculator;
   * ledger port;
   * external Billing clients.
5. Добавить architecture test, который запрещает импорты:

```text
internal/application/<package A>
    -> internal/application/<package B>
```

### Готово, когда

```bash
go list -deps ./...
go test ./internal/app/...
go test ./internal/application/...
```

проходят, а application-пакеты не импортируют друг друга.

---

## Шаг 2. Закрыть нарушения OpenAI-compatible core contract

Это нужно сделать до новых workers и native families.

### 2.1. Исправить `pricing_failed` после успешного upstream

Сейчас `llmrequest.Service` сохраняет `pricing_failed`, но затем возвращает Go error, из-за чего transport теряет успешный upstream response. 

Спецификация требует:

```text
upstream status сохраняется;
upstream body возвращается без изменений;
usage сохраняется как pricing_failed;
X-Billing-Status: pricing_failed;
auto-charge не запускается.
```



Нужно изменить application result так, чтобы post-upstream pricing failure являлся успешным forwarding result с отдельным billing state, а не transport-level error.

### 2.2. Завершить billing headers

Сейчас возвращается только часть usage dimensions и используются названия `X-Auto-Charge-*`. 

Добавить точный контракт:

```text
X-Billing-Input-Tokens
X-Billing-Cached-Input-Tokens
X-Billing-Output-Tokens
X-Billing-Reasoning-Tokens
X-Billing-Image-Input-Tokens
X-Billing-Audio-Input-Tokens
X-Billing-Audio-Output-Tokens
X-Billing-File-Input-Tokens
X-Billing-Video-Input-Tokens
X-Billing-Image-Generation-Units
X-Billing-Amount-Cents
X-Billing-Currency
X-Wallet-Balance-Cents
X-Wallet-Effective-Balance-Cents
X-Billing-Pending-Cents
X-Billing-Auto-Charge-Status
```



Headers должны строиться только из committed ledger state и результата balance admission/auto-charge.

### 2.3. Подключить minimum request balance

`AdmissionService` сейчас учитывает required reserve, но не принимает `MinRequestBalanceCents` в конфигурации. 

Добавить проверку:

```text
effective_balance >= estimated_client_amount
AND
effective_balance >= TOKENIO_MIN_REQUEST_BALANCE_CENTS
```



### 2.4. Подключить `last_used_at`

1. Обернуть public authenticator в существующий `UsageRecordingAuthenticator`.
2. Передать:

   * `APIKeyUsageRecorder`;
   * clock;
   * `TOKENIO_API_KEY_LAST_USED_TIMEOUT`.
3. Ошибка secondary update не должна отменять успешную authentication.

### 2.5. Завершить boundary tests

Покрыть:

```text
byte-for-byte request passthrough;
model-only rewrite;
byte-for-byte success response;
pricing_failed success passthrough;
полный набор billing headers;
missing Content-Type как JSON;
duplicate JSON keys;
depth > 128;
invalid UTF-8;
all idempotency states.
```

Structural parser обязан отклонять duplicate keys, trailing values и глубину более 128. 

### Готово, когда

Все OpenAI-compatible acceptance requirements `010`, `040` и соответствующие требования `050` имеют implementation и automated test.

---

## Шаг 3. Провести полный storage и migrations audit

### Сделать

Сопоставить `070` с migrations, domain structs и каждым Postgres adapter.

Проверить:

```text
users
api_keys
api_key_provisionings
resellers
routes
route_prices
usage_records
billing_sessions
billing_charge_batches
billing_charge_allocations
billing_charge_expected_records
forwarding_attempts
route_events
telegram_alerts
telegram_delivery_attempts
admin_audit_log
schema migrations table
```

Postgres является source of truth, а migrations должны быть explicit SQL. 

Особенно проверить:

* exact persistence всех десяти `EstimatedUsage` и `Usage` dimensions;
* partial indexes для pending и chargeable records;
* idempotency unique constraints;
* immutable batch command;
* ordered allocations и expected records;
* exact `float64` round trip для markup;
* nullable/non-null contracts;
* foreign keys;
* CAS predicates;
* canonical timestamps UTC.

Usage schema требует полное сохранение всех dimensions без synthetic zero reconstruction. 

### Integration tests migrations

```text
пустая PostgreSQL;
повторное применение;
upgrade с предыдущей schema version;
ошибка migration и rollback;
изменённый checksum;
несовместимая schema version;
параллельный запуск migrate;
gateway startup без требуемой migration.
```

### Готово, когда

`cmd/migrate` и schema validator имеют отдельные integration tests, а каждая таблица и constraint из `070` отмечены в implementation matrix.

---

## Шаг 4. Реализовать периодический billing recovery

Текущий `WorkerGraph` запускает только provisioning expiration и forwarding-attempt recovery. 

### Сделать

1. Создать application use case:

```text
BillingRecoveryService.RunCycle(ctx, limit)
```

2. Worker должен вызывать только этот use case.
3. За один цикл:

```text
1. найти persisted open batches;
2. восстановить pending batches;
3. повторить failed batches с тем же batch ID;
4. обработать succeeded replay без повторного Billing call;
5. продолжить partially_charged records;
6. загрузить новые billable candidates;
7. разбить их по provider_type + client_model + currency;
8. обработать все группы;
9. ограничить число операций cycle limit.
```

4. External Billing call выполняется только после committed `PrepareChargeBatch`.
5. После restart используется тот же stable financial command.
6. Worker не должен повторно отправлять LLM request.
7. Неопределённый результат Billing восстанавливается через тот же idempotency key.

Durable preparation, succeeded replay и historical/active claims подробно определены в `050`. 

### Обязательные crash-point tests

```text
process died before PrepareChargeBatch commit;
died after prepare, before Billing call;
died after Billing success, before ApplyChargeSuccess;
died during partial charge;
concurrent recovery cycles;
several provider/model groups;
failed batch retry;
succeeded replay without response balance;
no double charge after restart.
```

### Готово, когда

Billing recovery работает без поступления новых LLM requests и после любого restart продолжает persisted command без duplicate charge.

---

## Шаг 5. Завершить operational routing policy

### Сделать

1. Передать в routing/forwarding typed policy:

```text
UpstreamTimeout
UpstreamMaxAttempts
UpstreamMaxBackoff
RateLimitMaxWait
CooldownRateLimit
CooldownQuotaExceeded
Cooldown5XX
CooldownTimeout
CooldownAuthError
```

2. Ограничить общее число forwarding attempts.
3. Применять per-attempt timeout.
4. Retry разрешать только по normalized classifier и только до unsafe processing boundary.
5. Не retry-ить deterministic client 4xx.
6. Никогда не переходить в другую:

   * `api_family`;
   * `endpoint_kind`;
   * `client_model`.
7. При переходе на fallback:

   * завершить attempt;
   * освободить capacity;
   * atomically transfer reseller reserve;
   * создать новый attempt.
8. Реализовать automatic cooldown update.
9. Сохранять `route_event` для:

   * selected;
   * skipped;
   * retry;
   * failure;
   * success;
   * cooldown set/expired.
10. Подключить request, token и concurrent capacity limits.

Спецификация определяет retry boundary, cooldown reasons и route limits.  

### Архитектурное ограничение

Generic routing получает только normalized classification:

```text
rate_limit
quota_exceeded
auth_error
provider_5xx
timeout
connection_error
client_request_error
```

Provider-specific JSON parsing остаётся в adapter package.

### Готово, когда

Полностью выполнены 17 acceptance criteria routing specification. 

---

## Шаг 6. Завершить единый error model

### Сделать

1. Ввести один normalized application error contract.
2. Provider adapters возвращают классификацию, но не HTTP status публичного API.
3. Transport выполняет точный mapping по `080`.
4. Raw upstream error body никогда не возвращается клиенту.
5. Successful upstream body остаётся byte-for-byte неизменным.
6. Для всех ошибок после создания request ID возвращать:

   * `X-Local-Request-ID`;
   * `error.request_id`.
7. Исправить статусы:

```text
billing_unavailable before upstream -> 502
upstream_request_error              -> 400
upstream_unavailable                -> 502
no_route_available                  -> 503
pricing_unavailable                 -> 503
```

Полный registry и status mapping закреплены в `080`. 

### Tests

```text
auth;
validation;
routing;
billing before upstream;
billing after successful upstream;
pricing_failed;
every upstream classifier;
request ID;
no raw provider body;
no SQL/internal errors;
no credentials.
```

### Готово, когда

Любая gateway-owned ошибка имеет стабильный code и envelope, а provider failure не может попасть в public response как raw body. 

---

## Шаг 7. Завершить configuration и security wiring

### Сделать

1. Построить таблицу:

```text
Config field
env key
validation
runtime consumer
automated test
```

2. Не считать поле реализованным, пока оно не передано конкретному consumer.
3. Подключить:

   * upstream timeout/retry;
   * cooldowns;
   * rate-limit wait;
   * minimum request balance;
   * API-key last-used timeout;
   * Telegram settings;
   * logging level/format;
   * body logging policy.
4. В production запретить `TOKENIO_LOG_BODIES=true`.
5. Проверить redaction ключей с `TOKEN`, `SECRET`, `KEY`, `PASSWORD`, `DSN`, `AUTHORIZATION`.
6. Проверить:

   * HMAC secret обязателен;
   * provisioning encryption key — ровно 32 байта;
   * provisioning key отличается от HMAC secret;
   * production admin token удовлетворяет требованиям;
   * typed config неизменяем после старта.
7. Разделить migration execution и gateway startup согласно решению шага 0.

Config должен загружаться единожды, валидироваться fail-fast и передаваться как typed immutable value.  

### Готово, когда

Каждый config key из `090` либо используется runtime consumer, либо явно удалён из спецификации и кода.

---

## Шаг 8. Замкнуть Telegram vertical slice

### Сделать

1. Создать Telegram infrastructure graph:

   * sender;
   * alert store;
   * delivery-attempt store.
2. Собрать application services:

   * low-balance check;
   * alert delivery;
   * failed delivery retry;
   * stale attempt recovery;
   * periodic reseller balance scan.
3. После каждого committed изменения reseller balance запускать best-effort check.
4. Ошибка Telegram не должна откатывать:

   * reseller balance;
   * usage finalization;
   * charge success;
   * admin balance adjustment.
5. Periodic checker должен восстанавливать alerts, пропущенные post-commit вызовом.
6. Alert сначала сохраняется, затем отправляется.
7. Подключить в `WorkerGraph`:

   * pending delivery worker;
   * stale-attempt recovery;
   * periodic balance checker.
8. Если Telegram config отсутствует, весь Telegram graph выключается явно.
9. Добавить Admin API:

   * `GET /admin/v1/telegram-alerts`;
   * `POST /admin/v1/telegram-alerts/{id}/retry`.
10. Retry фиксировать в audit log.

Threshold и безопасное содержимое alert определены в routing spec. 

### Tests

```text
above threshold -> no alert;
below threshold -> pending alert;
deduplication;
send success;
temporary failure;
retry;
stale attempt recovery;
restart recovery;
financial transaction survives Telegram failure;
alert contains no secret.
```

---

## Шаг 9. Завершить Admin API и provisioning acceptance

### Admin API

Добавить отсутствующие endpoints:

```text
GET  /admin/v1/api-key-provisionings/{id}
GET  /admin/v1/route-events
GET  /admin/v1/telegram-alerts
POST /admin/v1/telegram-alerts/{id}/retry
```

Они прямо предусмотрены спецификацией.  

Для каждой изменяющей операции проверить:

```text
separate admin auth;
validation;
transaction;
exact before_state;
exact after_state;
reason;
CAS/locking;
audit commit in same transaction;
no raw secret in DTO.
```

### Provisioning

Закрыть acceptance matrix:

```text
first creation;
same-input replay returns same raw key;
different-input replay -> 409;
existing active key -> already_provisioned;
disabled user rejection;
delivery_attempt recorded before raw response;
confirm delivery idempotency;
encrypted material cleared;
expired provisioning -> 410;
delivered provisioning never returns raw key;
expiration worker immediate startup cycle;
service token rejected by public API;
raw key absent from logs and audit.
```

Provisioning transaction и replay contract определены в `021`. 

### Готово, когда

Все dangerous Admin operations имеют transactional audit, а provisioning lifecycle полностью проверен на реальном PostgreSQL.

---

## Шаг 10. Реализовать native API families

Начинать только после ADR по native auth.

### Общая архитектура

Не изменять generic `llmrequest` под vendor-specific поля.

Для каждой family создать отдельные:

```text
inbound path adapter;
request metadata parser;
model identifier rewriter;
forwarding adapter;
usage extractor;
error classifier;
safe request header policy;
safe response header policy;
catalog response adapter.
```

Generic pipeline получает только:

```text
api_family
endpoint_kind
client_model
requested capabilities
opaque original body
normalized usage
normalized forwarding failure
```

### 10.1. Anthropic native

Реализовать:

```text
POST /v1/messages
api_family = anthropic_native
endpoint_kind = chat
model = body.model
```

Затем acceptance:

```text
body passthrough;
model-only rewrite;
usage extraction;
error normalization;
stream=true rejection;
fallback only anthropic_native.
```

### 10.2. Gemini native

Реализовать:

```text
POST /v1beta/models/{model}:generateContent
POST /v1beta/models/{model}:embedContent
POST /v1beta/models/{model}:batchEmbedContents
GET  /v1beta/models
```

До появления streaming spec:

```text
POST ...:streamGenerateContent
-> 400 streaming_unsupported
```

Model извлекается и при необходимости переписывается только в path segment.

### 10.3. Ollama native

Реализовать:

```text
POST /api/chat
POST /api/generate
POST /api/embeddings
GET  /api/tags
```

Для Ollama отдельно определить conservative usage estimation, поскольку native responses могут не содержать полный billable usage.

Стандартные paths и acceptance criteria закреплены в `011`.  

### Готово, когда

Каждая family проходит одинаковые проверки:

```text
path detection;
auth normalization;
model extraction;
model rewrite boundary;
request passthrough;
response passthrough;
usage/pricing;
error classification;
same-family fallback;
no cross-family route.
```

---

## Шаг 11. Создать воспроизводимый integration environment

### Состав

```text
PostgreSQL
fake Billing Service
fake OpenAI-compatible upstream
fake Anthropic upstream
fake Gemini upstream
fake Ollama upstream
fake Telegram API
gateway
migration command
```

### Fake services должны воспроизводить

```text
success;
401/403;
429;
quota exceeded;
5xx;
timeout;
connection reset;
response headers received then body failure;
missing usage;
malformed usage;
aggregate usage;
partial wallet balance;
duplicate charge request;
successful charge without returned balance;
uncertain charge result;
Telegram temporary failure.
```

### Tests

Запускать через один command, например:

```bash
go test -tags=integration ./integration/...
```

Ни один acceptance test не должен обращаться к реальному LLM provider или Telegram.

---

## Шаг 12. Выполнить полный spec-to-code audit

Для каждого требования из:

```text
000
010
011
020
021
030
040
050
060
070
080
090
ADR
```

зафиксировать ровно один результат:

```text
implemented + automated test;
или
explicitly unsupported by current specification.
```

Недопустимые статусы:

```text
interface exists;
repository method exists;
struct exists;
planned;
probably works.
```

Результат считается выполненным только при наличии executable evidence.

---

## Шаг 13. Финальная production verification

Выполнить на чистом checkout точного commit:

```bash
gofmt -w .
go vet ./...
go test ./...
go test -race ./...
go build ./cmd/gateway
go build ./cmd/migrate
```

Затем проверить:

```text
migration на пустой PostgreSQL;
повторный migrate;
upgrade предыдущей schema;
gateway startup с актуальной schema;
gateway отказ с несовместимой schema;
graceful shutdown;
billing recovery после restart;
provisioning expiration после restart;
Telegram recovery после restart;
parallel reservations;
parallel charge recovery;
отсутствие duplicate charge;
byte-for-byte request passthrough;
byte-for-byte successful response passthrough;
model-only rewrite;
полный public error registry;
полный billing-header contract;
отсутствие secrets в logs;
реальные HTTP acceptance requests ко всем endpoint families.
```

## Рекомендуемый порядок PR

```text
PR-01  Spec conflicts, ADRs, implementation matrix
PR-02  Application dependency cleanup
PR-03  pricing_failed success passthrough
PR-04  Billing headers, min balance, last_used_at
PR-05  Storage and migration contract audit
PR-06  Periodic billing recovery
PR-07  Operational routing policy and route events
PR-08  Unified error model
PR-09  Configuration and security wiring
PR-10  Telegram application/runtime wiring
PR-11  Telegram workers and Admin API
PR-12  Admin and provisioning acceptance gaps
PR-13  Anthropic-native
PR-14  Gemini-native
PR-15  Ollama-native
PR-16  Reproducible integration environment
PR-17  Final specification matrix and production verification
```

Каждый PR должен закрывать один архитектурный инвариант, содержать собственные unit/integration tests и оставлять `go test ./...`, `go test -race ./...`, `go vet ./...` и `go build ./cmd/gateway` зелёными.
