## 1. Закрыть отсутствующие контракты хранения

Сверить текущие migrations и repositories с `070-database-schema`.

Добавить только отсутствующие:

* таблицы и поля;
* foreign keys и `CHECK`;
* unique constraints для idempotency;
* индексы для route lookup, pending usage и charge batches;
* atomic reseller balance operations;
* route events и Telegram alert state.

Проверить применение migrations на пустой БД и повторный запуск без изменений.

---

## 2. Создать единый `llmrequest` orchestrator

Создать:

```text
internal/application/llmrequest
```

Он должен выполнять:

```text
authentication
→ request parsing
→ capability detection
→ route selection
→ preflight pricing
→ billing admission
→ usage reservation
→ forwarding
→ usage extraction
→ pricing
→ ledger finalization
→ auto-charge trigger
→ response passthrough
```

Orchestrator не должен:

```text
знать HTTP;
импортировать Postgres;
читать environment;
знать конкретного provider;
формировать provider-specific headers;
конвертировать API formats.
```

`internal/app` только собирает зависимости и передаёт их orchestrator.

---

## 3. Завершить ledger и idempotency

Реализовать атомарные операции:

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

Обеспечить:

* usage record создаётся до forwarding;
* резервируется пользовательская сумма;
* резервируется reseller cost;
* final amount корректирует reservation;
* повторная finalization невозможна;
* один idempotency key не создаёт два usage records;
* один usage record не списывается дважды;
* одинаковый key с другим request fingerprint возвращает conflict;
* partial charge остаётся доступным для следующего списания;
* `BillingChargeRequestID` закрепляется только за реально включёнными allocations.

---

## 4. Завершить auto-charge

Исправить `AutoChargeService.Run()`:

* обрабатывать все группы `provider_type + client_model`;
* создавать отдельный charge batch для каждой группы;
* не завершаться после первой группы;
* использовать idempotency key при вызове Billing Service;
* применять только фактические allocations;
* сохранять остаток после partial charge.

Добавить один периодический billing worker, который:

```text
подбирает pending usage;
повторяет failed batches;
обрабатывает uncertain results;
возобновляет partially charged records.
```

Worker должен восстанавливать списания после перезапуска процесса.

---

## 5. Реализовать provider adapter boundary

Определить contracts:

```text
Forwarder
UsageExtractor
ErrorClassifier
TokenEstimator
ModelRewriter
SafeHeaderPolicy
```

Создать общий:

```text
OpenAICompatibleAdapter
```

Он должен:

* получать уже resolved reseller credential;
* заменять client `Authorization`;
* удалять hop-by-hop и небезопасные headers;
* сохранять request body;
* изменять только `model`, если разрешён `model_rewrite_policy`;
* возвращать upstream status и body без преобразования.

Не создавать отдельный adapter для каждого OpenAI-compatible reseller, пока его HTTP-контракт действительно не отличается.

---

## 6. Реализовать полный `POST /v1/chat/completions`

Transport должен только:

```text
проверить method/path;
проверить Bearer sk_...;
ограничить размер body;
прочитать body;
вызвать llmrequest.Execute;
записать status, safe headers и original body.
```

Полный сценарий:

```text
API key
→ user
→ billing identity
→ body.model
→ requested capabilities
→ compatible routes
→ cheapest available route
→ balance checks
→ reservation
→ upstream request
→ usage
→ final price
→ ledger
→ auto-charge
→ billing headers
```

Проверить ошибки:

```text
unauthorized
invalid_api_key
user_disabled
model_required
unknown_model
unsupported_capability
no_route_available
insufficient_balance
upstream_error
```

---

## 7. Реализовать routing, fallback, cooldown и route limits

Route lookup должен использовать:

```text
api_family
+ endpoint_kind
+ client_model
```

Фильтровать routes по:

* enabled;
* capabilities;
* reseller enabled;
* reseller secret present;
* reseller effective balance;
* route price;
* cooldown;
* `requests_per_minute`;
* `tokens_per_minute`;
* `concurrent_requests`.

Выбирать самый дешёвый доступный route.

Fallback разрешать только при совпадении:

```text
api_family
endpoint_kind
client_model
requested capabilities
```

Добавить классификацию:

```text
rate_limit
quota_exceeded
auth_error
temporary_5xx
timeout
client_error
unknown_result
```

На retry:

* сохранять тот же usage record;
* записывать новый route attempt;
* освобождать неиспользованный reserve предыдущего route;
* не превышать configured max attempts.

---

## 8. Реализовать usage extraction, estimation и pricing

Поддержать:

```text
input tokens
cached input tokens
output tokens
reasoning tokens
embedding input tokens
image generation units
multimodal input
```

Правила:

* использовать actual usage, когда он есть;
* использовать conservative estimation, когда usage отсутствует;
* применять safety factors только к estimation;
* считать деньги в RUB cents;
* не использовать `float64` как итоговый денежный тип;
* применять markup один раз;
* округлять один раз в конце;
* successful response не может получить случайную нулевую стоимость.

Один calculator должен использоваться в:

```text
/v1/models pricing
request preflight
final billing
```

---

## 9. Подключить остальные OpenAI-compatible endpoints

На том же `llmrequest` pipeline реализовать:

```text
POST /v1/embeddings
POST /v1/images/generations
```

Для embeddings:

* требовать capability `embeddings`;
* учитывать input usage;
* возвращать upstream body без изменений.

Для image generation:

* требовать capability `images_generation`;
* извлекать usage либо считать conservative units;
* не изменять response body.

Обязательный первый public surface: `/health`, `/v1/models` и три POST endpoint. Обязательны также API keys, billing identity, routing, capability validation, ledger, auto-charge, idempotency, reseller balance accounting, Telegram alerts и Admin API. 

---

## 10. Довести authentication и billing identity

Проверить:

```text
Authorization: Bearer sk_...
```

Обеспечить:

* `HMAC-SHA256(TOKENIO_API_KEY_HASH_SECRET, raw_key)`;
* отсутствие SHA-256 fallback;
* проверку key enabled/revoked;
* проверку user enabled;
* получение `billing_subject_user_id`;
* короткоживущий billing JWT;
* корректные `iss`, `aud`, `iat`, `exp`;
* client API key никогда не передаётся reseller;
* billing JWT никогда не возвращается клиенту;
* raw keys не сохраняются и не логируются.

---

## 11. Довести configuration и security contracts

Реализовать typed config и fail-fast validation для:

```text
database
billing service
billing JWT
admin token
API-key HMAC secret
provisioning
request body limit
HTTP timeouts
upstream timeout
retry limits
cooldown durations
route limits
Telegram alerts
logging
```

Обеспечить:

* environment читается только в config/bootstrap;
* reseller keys разрешаются через dedicated secret resolver;
* production body logging запрещён;
* secret values редактируются в logs;
* invalid required config останавливает запуск;
* reseller `base_url` проходит URL/SSRF validation;
* production upstream требует HTTPS;
* redirects контролируются или запрещаются.

---

## 12. Довести Admin API

Закрыть операции из спецификации:

```text
users
API keys
API-key provisionings
resellers
reseller balances
routes
route cooldowns
route prices
usage records
billing charge batches
manual financial resolution
audit log
```

Для изменяющих операций:

* обязательная авторизация admin token;
* валидация входных данных;
* transaction;
* audit record;
* reason для ручных финансовых изменений;
* отсутствие raw secrets в response.

Не добавлять UI и дополнительные diagnostic endpoints.

---

## 13. Завершить API-key provisioning

Реализовать lifecycle:

```text
trusted service request
→ idempotent provisioning
→ encrypted temporary raw key
→ delivery
→ delivery confirmation
→ deletion of recoverable key material
```

Обеспечить:

* retry возвращает тот же raw key до confirmation;
* повтор не создаёт второй API key;
* provisioning имеет expiration;
* worker удаляет expired material;
* delivered/expired key больше нельзя получить;
* service token не используется как public auth;
* все операции аудитируются.

---

## 14. Реализовать Telegram reseller balance alerts

Добавить:

* проверку reseller balance после изменения;
* configured low-balance threshold;
* deduplication interval;
* сохранение alert state;
* Telegram sender;
* периодическую повторную проверку.

Ошибка Telegram не должна:

```text
ломать LLM request;
откатывать ledger;
откатывать reseller balance;
блокировать auto-charge.
```

---

## 15. Реализовать Anthropic-native family

Добавить:

```text
POST /v1/messages
```

Реализовать:

* path → `anthropic_native`;
* endpoint kind `chat`;
* model extraction из `body.model`;
* Anthropic forwarding adapter;
* Anthropic usage extractor;
* Anthropic error classifier;
* model rewrite только при явной policy;
* запрет fallback в OpenAI-compatible family;
* original response body passthrough.

---

## 16. Реализовать Gemini-native family

Добавить:

```text
POST /v1beta/models/{model}:generateContent
POST /v1beta/models/{model}:embedContent
POST /v1beta/models/{model}:batchEmbedContents
GET  /v1beta/models
```

Для:

```text
/v1beta/models/{model}:streamGenerateContent
```

до появления streaming specification возвращать:

```text
HTTP 400
error.code = streaming_unsupported
```

Реализовать:

* deterministic model extraction из path;
* Gemini adapter;
* usage extractor;
* error classifier;
* path model rewrite только при explicit policy;
* запрет fallback между API families.

---

## 17. Реализовать Ollama-native family

Добавить:

```text
POST /api/chat
POST /api/generate
POST /api/embeddings
GET  /api/tags
```

Реализовать:

* path → `ollama_native`;
* model extraction из `body.model`;
* Ollama adapter;
* usage extraction или estimation;
* original response passthrough;
* запрет fallback в другие API families.

---

## 18. Закрыть единый error model

Для всех public endpoints унифицировать:

```json
{
  "error": {
    "code": "...",
    "message": "...",
    "request_id": "..."
  }
}
```

Обеспечить deterministic mapping для:

```text
auth errors;
validation errors;
routing errors;
balance errors;
capability errors;
rate limits;
upstream errors;
billing errors;
idempotency conflicts;
internal errors.
```

Не возвращать клиенту:

```text
SQL errors;
provider credentials;
reseller identifiers;
provider_model;
internal stack traces;
billing JWT;
cooldown reason details.
```

---

## 19. Добавить обязательные acceptance tests

### Public API

```text
/health
/v1/models
/chat/completions
/embeddings
/images/generations
```

### Passthrough

```text
request body unchanged;
only model rewritten;
response body unchanged;
unsafe headers removed;
billing headers added.
```

### Routing

```text
cheapest compatible route;
capability filtering;
cooldown filtering;
missing secret filtering;
fallback inside same family;
no cross-family fallback.
```

### Finance

```text
reservation;
finalization;
release of unused reserve;
reseller balance;
idempotency;
parallel requests;
partial charge;
all charge groups;
retry after failure;
no double charge.
```

### Control plane

```text
Admin API;
provisioning retry;
provisioning expiration;
audit;
config validation;
Telegram deduplication.
```

### Native families

```text
path detection;
model extraction;
correct api_family;
response passthrough;
no cross-family fallback.
```

---

## 20. Финальная проверка соответствия

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

должен существовать один из результатов:

```text
implementation + test;
явно задокументированное unsupported поведение, разрешённое спецификацией.
```

Финальные команды:

```bash
gofmt -w .
go vet ./...
go test ./...
go test -race ./...
go build ./cmd/gateway
```