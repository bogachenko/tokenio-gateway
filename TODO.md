# TODO — оставшиеся задачи Tokenio Gateway

## 1. Подключить публичный LLM HTTP transport

Создать единый transport adapter для LLM endpoints.

Transport должен отвечать только за:

```text
определение path и method;
создание local_request_id;
проверку Authorization: Bearer sk_...;
ограничение размера request body;
чтение исходного body;
чтение Idempotency-Key;
вызов llmrequest orchestrator;
запись upstream status;
фильтрацию response headers;
добавление billing headers;
возврат исходного upstream body.
```

Transport не должен:

```text
выбирать route;
рассчитывать стоимость;
обращаться напрямую к PostgreSQL;
обращаться напрямую к Billing Service;
знать конкретного provider;
изменять semantic payload;
выполнять ledger transitions.
```

Добавить маршруты:

```text
POST /v1/chat/completions
POST /v1/embeddings
POST /v1/images/generations
```

Обновить root router, чтобы эти endpoints передавались в public LLM handler, а не возвращали `404`.

---

## 2. Завершить `POST /v1/chat/completions`

Подключить существующий `llmrequest` pipeline к HTTP endpoint.

Полный сценарий:

```text
API key authentication
→ request parsing
→ capability detection
→ route selection
→ preflight pricing
→ billing admission
→ user reservation
→ reseller reservation
→ forwarding
→ usage extraction
→ final pricing
→ ledger finalization
→ auto-charge trigger
→ response passthrough
```

Обеспечить ошибки:

```text
401 unauthorized
401 invalid_api_key
403 user_disabled
400 invalid_json
400 model_required
400 streaming_unsupported
400 unknown_model
400 unsupported_capability
409 idempotency_conflict
402 insufficient_balance
503 no_route_available
502 upstream_error
504 upstream_timeout
500 internal_error
```

Successful upstream response body должен возвращаться byte-for-byte без преобразования.

---

## 3. Подключить embeddings и image generation

На том же `llmrequest` pipeline реализовать:

```text
POST /v1/embeddings
POST /v1/images/generations
```

Для embeddings:

```text
endpoint_kind = embeddings;
обязательная capability embeddings;
model извлекается из body.model;
учитывается input usage;
response body не изменяется.
```

Для image generation:

```text
endpoint_kind = images_generation;
обязательная capability images_generation;
извлекается actual usage либо conservative generation units;
response body не изменяется.
```

Не создавать отдельные orchestrators для каждого endpoint.

---

## 4. Завершить ledger и idempotency

Проверить и закрыть атомарные операции:

```text
CreateOrGetUsage
ReserveUsage
RecordForwardingAttempt
FinalizeUsage
ReleaseReservation
MoveToPendingCharge
ApplyChargeAllocation
MarkFailed
```

Обеспечить инварианты:

```text
usage record создаётся до первого forwarding attempt;
один idempotency scope создаёт не более одного usage record;
одинаковый key с другим request fingerprint возвращает conflict;
повторная finalization невозможна;
один usage record не списывается дважды;
user reservation и reseller reservation выполняются атомарно;
unused reservation освобождается;
final amount корректирует reservation;
retry использует тот же usage record;
каждый forwarding attempt сохраняется отдельно.
```

Idempotency scope:

```text
user_id + endpoint_kind + idempotency_key
```

Request fingerprint должен строиться детерминированно из исходного запроса и routing-relevant metadata.

---

## 5. Закрыть partial charge contract

Исправить и проверить:

```text
BillingChargeRequestID назначается только records, реально вошедшим в allocations;
partial charge уменьшает RemainingAmountCents;
частично оплаченный record снова доступен следующему charge batch;
records без allocation не блокируются;
ApplyChargeSuccess применяет только фактические allocations;
один allocation не может быть применён повторно.
```

`ExpectedRecords` не должен использоваться как основание для claim всех records группы.

Source of truth для списанной суммы:

```text
persisted charge allocations
```

---

## 6. Добавить периодический billing recovery worker

Создать один worker, который периодически:

```text
находит pending usage;
создаёт charge batches по provider_type + client_model;
обрабатывает все группы;
повторяет failed batches;
восстанавливает uncertain batches;
продолжает partially charged records;
завершает подготовленные, но не отправленные batches.
```

Worker должен:

```text
переживать restart процесса;
использовать persisted state;
быть идемпотентным;
не создавать duplicate charge;
не зависеть от поступления нового LLM request;
обрабатывать ограниченное число batches за один цикл;
иметь configurable interval и batch limit.
```

Нельзя хранить состояние recovery только в памяти процесса.

---

## 7. Устранить связи между application packages

Убрать прямые импорты:

```text
internal/application/billing
→ internal/application/ledger

internal/application/billing
→ internal/application/pricing
```

Общие контракты и значения вынести в:

```text
domain
или consumer-owned ports
```

Application packages не должны вызывать соседние application packages как инфраструктурные библиотеки.

`internal/app` должен собирать use cases через интерфейсы.

---

## 8. Проверить routing, fallback и route capacity

Добавить полные тесты для route filtering по:

```text
api_family;
endpoint_kind;
client_model;
requested capabilities;
route enabled;
reseller enabled;
credential present;
route price present;
currency;
reseller effective balance;
cooldown;
requests_per_minute;
tokens_per_minute;
concurrent_requests;
model rewrite support.
```

Fallback разрешать только при полном совпадении:

```text
api_family
endpoint_kind
client_model
requested capabilities
```

Retry должен:

```text
использовать тот же usage record;
создавать новый forwarding attempt;
освобождать reserve неиспользованного route;
соблюдать max attempts;
не повторять client_error;
не переходить в другую API family.
```

---

## 9. Закрыть usage extraction и estimation

Проверить поддержку:

```text
input tokens;
cached input tokens;
output tokens;
reasoning tokens;
embedding input tokens;
image generation units;
image input;
audio input;
audio output;
file input;
video input.
```

Обеспечить:

```text
actual usage имеет приоритет;
estimation применяется только при отсутствии или неполноте actual usage;
safety factor применяется только к estimation;
successful billable response не получает случайную нулевую цену;
markup применяется один раз;
округление выполняется один раз;
финальные деньги хранятся как integer RUB cents.
```

Один pricing calculator должен использоваться в:

```text
GET /v1/models;
request preflight;
final usage pricing.
```

---

## 10. Завершить Telegram reseller balance alerts

Реализовать:

```text
Telegram sender;
application service проверки balance;
вызов проверки после каждого изменения reseller balance;
периодический balance checker;
low-balance threshold;
deduplication interval;
persisted alert state;
recovery после временной ошибки Telegram.
```

Ошибка отправки Telegram не должна:

```text
ломать LLM request;
откатывать ledger;
откатывать reseller balance;
останавливать auto-charge;
менять результат финансовой транзакции.
```

Telegram sender должен находиться в infrastructure и реализовывать application-owned port.

---

## 11. Проверить migrations и storage contracts

Сопоставить migrations и repositories с:

```text
docs/spec/070-database-schema.ru.md
```

Проверить наличие:

```text
всех таблиц и колонок;
foreign keys;
CHECK constraints;
unique constraints для idempotency;
индексов route lookup;
индексов pending usage;
индексов open charge batches;
atomic reseller balance operations;
route events;
Telegram alert state;
provisioning expiration indexes.
```

Проверить:

```text
применение migrations на пустой PostgreSQL;
повторный запуск migrations;
upgrade с предыдущей схемы;
rollback транзакции при ошибке migration;
соответствие nullable/non-null полей repository contracts.
```

---

## 12. Завершить единый public error model

Все gateway-owned public errors должны иметь форму:

```json
{
  "error": {
    "code": "unknown_model",
    "message": "Unknown model",
    "request_id": "llmreq_..."
  }
}
```

Добавить единый deterministic mapping для:

```text
authentication;
validation;
routing;
capabilities;
balance;
idempotency;
rate limits;
capacity;
upstream failures;
billing failures;
storage failures;
internal contract violations.
```

Не возвращать клиенту:

```text
SQL errors;
raw repository errors;
provider credentials;
reseller_id;
route_id;
provider_model;
billing JWT;
stack trace;
internal cooldown reason;
internal configuration values.
```

`X-Local-Request-ID` должен возвращаться для всех ответов после создания request ID.

---

## 13. Завершить configuration и security verification

Проверить typed config и fail-fast validation для:

```text
database;
billing service;
billing JWT;
billing charge service token;
admin token;
API-key HMAC secret;
provisioning service token;
provisioning encryption key;
request body limit;
HTTP timeouts;
upstream timeout;
retry limits;
cooldown;
route limits;
billing worker;
Telegram;
logging.
```

Обеспечить:

```text
environment читается только в config/bootstrap;
нет SHA-256 fallback для API keys;
reseller secrets разрешаются через SecretResolver;
raw secrets не логируются;
production body logging запрещён;
invalid required config останавливает запуск;
reseller base_url проходит SSRF validation;
production upstream требует HTTPS;
HTTP redirects запрещены либо валидируются повторно;
private и loopback адреса запрещены для external resellers.
```

---

## 14. Завершить Admin API проверками и тестами

Проверить операции:

```text
users;
API keys;
API-key provisionings;
resellers;
reseller balances;
routes;
route cooldowns;
route prices;
usage records;
billing charge batches;
manual financial resolution;
audit log.
```

Для каждой изменяющей операции обеспечить:

```text
admin authentication;
input validation;
transaction boundary;
audit record;
reason для manual financial mutation;
optimistic или pessimistic concurrency control;
отсутствие raw secrets в response;
детерминированный error mapping.
```

Не добавлять UI и временные diagnostic endpoints.

---

## 15. Завершить API-key provisioning acceptance contract

Проверить lifecycle:

```text
trusted service request
→ idempotent provisioning
→ API key creation
→ encrypted temporary raw material
→ delivery
→ delivery confirmation
→ deletion of recoverable material
```

Проверить:

```text
retry до confirmation возвращает тот же raw key;
retry не создаёт второй API key;
один payment_order_id соответствует одной provisioning operation;
expiration worker удаляет recoverable material;
delivered key нельзя получить повторно;
expired key нельзя получить;
service token не принимается public endpoints;
операции создают audit records.
```

---

## 16. Реализовать Anthropic-native family

Добавить:

```text
POST /v1/messages
```

Реализовать отдельные family-specific adapters:

```text
request metadata parser;
model extraction из body.model;
forwarding adapter;
usage extractor;
error classifier;
safe header policy;
model rewriter.
```

Обеспечить:

```text
api_family = anthropic_native;
endpoint_kind = chat;
original request body passthrough;
original response body passthrough;
model rewrite только при explicit policy;
fallback только внутри anthropic_native;
streaming до отдельной спецификации отклоняется.
```

Generic `llmrequest` orchestrator изменять под Anthropic-specific поля нельзя.

---

## 17. Реализовать Gemini-native family

Добавить:

```text
POST /v1beta/models/{model}:generateContent
POST /v1beta/models/{model}:embedContent
POST /v1beta/models/{model}:batchEmbedContents
GET  /v1beta/models
```

До реализации streaming:

```text
POST /v1beta/models/{model}:streamGenerateContent
→ 400 streaming_unsupported
```

Реализовать:

```text
deterministic path parser;
model extraction из path;
Gemini forwarding adapter;
usage extractor;
error classifier;
safe header policy;
path model rewrite при explicit policy.
```

Fallback между Gemini-native и другими API families запрещён.

---

## 18. Реализовать Ollama-native family

Добавить:

```text
POST /api/chat
POST /api/generate
POST /api/embeddings
GET  /api/tags
```

Реализовать:

```text
path detection;
model extraction из body.model;
Ollama forwarding adapter;
usage extraction;
conservative estimation;
error classifier;
response passthrough.
```

Fallback разрешён только внутри `ollama_native`.

---

## 19. Добавить обязательные acceptance tests

### Public API

```text
GET /health;
GET /v1/models;
POST /v1/chat/completions;
POST /v1/embeddings;
POST /v1/images/generations.
```

### Passthrough

```text
request body byte-for-byte unchanged;
изменяется только model identifier;
response body byte-for-byte unchanged;
unsafe headers удаляются;
reseller Authorization не возвращается;
billing headers добавляются.
```

### Authentication

```text
missing Authorization;
malformed Bearer;
unknown key;
disabled key;
revoked key;
expired key;
disabled user;
HMAC lookup;
raw key не логируется.
```

### Routing

```text
cheapest compatible route;
capability filtering;
cooldown filtering;
missing secret filtering;
balance filtering;
route capacity;
fallback same family;
no cross-family fallback;
max attempts.
```

### Ledger и финансы

```text
reservation;
finalization;
failed forwarding;
unused reserve release;
reseller balance update;
idempotency replay;
idempotency conflict;
parallel requests;
partial charge;
all charge groups;
failed batch retry;
uncertain batch recovery;
no double charge;
restart recovery.
```

### Control plane

```text
Admin API;
audit;
manual financial reason;
provisioning retry;
provisioning expiration;
config validation;
Telegram deduplication.
```

### Native families

```text
path detection;
model extraction;
correct api_family;
model rewrite boundary;
response passthrough;
no cross-family fallback.
```

Тесты финансовых операций запускать также с:

```bash
go test -race ./...
```

---

## 20. Добавить integration environment

Подготовить воспроизводимый integration setup:

```text
PostgreSQL;
fake Billing Service;
fake OpenAI-compatible upstream;
fake Anthropic upstream;
fake Gemini upstream;
fake Ollama upstream;
fake Telegram API.
```

Fake services должны позволять детерминированно воспроизводить:

```text
success;
429;
401/403 upstream auth;
5xx;
timeout;
connection reset;
malformed usage;
missing usage;
partial billing balance;
duplicate billing request;
uncertain billing result.
```

Acceptance tests не должны зависеть от реальных provider APIs.

---

## 21. Выполнить финальную проверку спецификаций

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
```

зафиксировать один результат:

```text
implementation + automated test;
или явно разрешённое спецификацией unsupported поведение.
```

Не считать требование выполненным только по наличию интерфейса, структуры или repository method.

---

## 22. Финальная production verification

Выполнить:

```bash
gofmt -w .
go vet ./...
go test ./...
go test -race ./...
go build ./cmd/gateway
```

Дополнительно проверить:

```text
запуск на пустой PostgreSQL;
повторный запуск migrations;
graceful shutdown;
billing worker restart recovery;
provisioning worker restart recovery;
отсутствие raw secrets в logs;
работу public endpoints через реальные HTTP requests;
корректность body passthrough;
корректность financial reconciliation.
```

## Рекомендуемый порядок реализации

```text
1. Public LLM HTTP transport
2. POST /v1/chat/completions
3. Embeddings и images endpoints
4. Ledger/idempotency invariants
5. Partial charge contract
6. Billing recovery worker
7. Application dependency cleanup
8. Routing и pricing acceptance tests
9. Telegram alerts
10. Storage/migrations verification
11. Error model и security verification
12. Admin/provisioning acceptance tests
13. Anthropic-native
14. Gemini-native
15. Ollama-native
16. Полный integration test environment
17. Финальная проверка спецификаций
```