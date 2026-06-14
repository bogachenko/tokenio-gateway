## Проблема в слое

В проекте уже реализованы отдельные application-сервисы, Postgres-репозитории, provisioning, admin API, pricing, routing, ledger и forwarding adapter, но отсутствует единый application-level use case, который собирает их в сквозную обработку LLM-запроса.

Основные блокеры:

* нет SQL migrations, хотя они подключены через `//go:embed *.up.sql`;
* нет публичных POST endpoints;
* forwarding adapter не включён в runtime;
* auto-charge не запускается как часть request pipeline или worker;
* отсутствуют тесты;
* отсутствует production/deployment-контур.

Спецификация требует сквозной процесс от аутентификации и route selection до ledger, auto-charge и возврата неизменённого upstream response body. 

## Инвариант

Каждый этап реализации должен сохранять следующие правила:

```text
request API format сохраняется;
semantic payload не конвертируется;
разрешён только explicit model identifier rewrite;
fallback не пересекает api_family;
Postgres — source of truth;
billing невозможен без локального usage record;
transport не выполняет routing, pricing и ledger logic;
provider-specific поведение находится только в adapters;
raw credentials не сохраняются и не логируются;
response body upstream не изменяется;
billing metadata возвращается только в headers.
```

## Неправильное решение

Нельзя начинать с добавления трёх HTTP handlers, которые напрямую:

```text
читают Postgres;
выбирают route;
вызывают reseller;
считают цену;
запускают billing;
формируют ledger.
```

Это создаст второй набор бизнес-правил в transport и оставит существующие application-компоненты разрозненными.

Также нельзя:

* добавлять provider-specific `switch` в generic handler;
* запускать billing до фиксации usage;
* прикреплять charge request ко всем records группы, даже если они не вошли в allocations;
* завершать auto-charge после обработки первой billing group;
* связывать application packages прямыми импортами друг друга;
* реализовывать native APIs через конвертацию в OpenAI-compatible формат.

## Правильное решение

Реализацию нужно вести вертикальными срезами, начиная с базы и одного полностью рабочего `POST /v1/chat/completions`. После завершения первого среза остальные endpoints подключаются к тому же generic orchestration pipeline.

# Пошаговый план полной реализации

## Этап 1. Зафиксировать карту спецификации

Создать:

```text
docs/implementation-status.md
```

Для каждого требования из:

```text
000-tokenio-gateway.ru.md
010-external-api.ru.md
011-native-api-families.ru.md
020-auth-and-billing-identity.ru.md
021-api-key-provisioning.ru.md
030-routing-and-resellers.ru.md
040-pricing-and-usage.ru.md
050-ledger-and-auto-charge.ru.md
060-admin-api.ru.md
070-database-schema.ru.md
080-error-model.ru.md
090-configuration.ru.md
```

зафиксировать:

```text
requirement;
ответственный слой;
реализующий package;
тест;
статус: missing / partial / complete;
блокирующие зависимости.
```

Одновременно устранить противоречия между draft-спецификациями и перевести согласованные документы в `Accepted`.

### Результат

Ни одно требование не реализуется только потому, что оно случайно обнаружилось в процессе разработки.

---

## Этап 2. Восстановить собираемость проекта

Добавить реальные migrations:

```text
db/migrations/000001_initial_schema.up.sql
db/migrations/000001_initial_schema.down.sql
```

Если схема слишком большая, разделить:

```text
000001_users_and_api_keys
000002_provisioning
000003_resellers_and_routes
000004_route_prices
000005_usage_ledger
000006_billing_charge_batches
000007_route_events
000008_telegram_alerts
000009_admin_audit
```

Реализовать:

* создание всех таблиц из `070-database-schema`;
* foreign keys;
* `CHECK` constraints;
* unique constraints;
* индексы для runtime queries;
* partial indexes для pending records;
* timestamps;
* migration transaction;
* advisory lock для одновременного запуска нескольких экземпляров;
* таблицу версии migrations.

### Обязательные проверки

```bash
go test ./...
go vet ./...
go build ./cmd/gateway
```

Integration test должен:

1. запустить пустой Postgres;
2. применить migrations;
3. проверить схему;
4. повторно запустить migrations;
5. убедиться в идемпотентности migration runner.

### Результат

Проект собирается и запускается на чистой БД.

---

## Этап 3. Исправить границы application packages

Сейчас billing, ledger и pricing не должны превращаться во взаимозависимый application-монолит.

Создать orchestration package:

```text
internal/application/llmrequest
```

Он имеет право координировать:

```text
authentication;
request parsing;
billing admission;
route loading;
route selection;
preflight pricing;
ledger reservation;
forwarding;
usage extraction;
final pricing;
ledger finalization;
auto-charge trigger.
```

Он не должен:

```text
знать HTTP;
знать Postgres;
знать конкретного provider;
читать environment;
формировать provider-specific headers;
парсить provider-specific usage.
```

Определить consumer-owned interfaces внутри `llmrequest`, например:

```text
Authenticator
BalanceAdmission
RouteCandidateStore
RouteSelector
UsageReservation
ForwarderRegistry
UsageExtractorRegistry
PriceCalculator
UsageFinalizer
AutoChargeTrigger
```

`internal/app` должен только создать реализации и передать их orchestrator.

### Результат

Application packages не импортируют другие application packages ради доступа к concrete service. Связь проходит через orchestration contract и интерфейсы.

---

## Этап 4. Закрыть транзакционные инварианты ledger

До подключения HTTP необходимо закончить ledger state machine.

Полный lifecycle:

```text
created
→ reserved
→ forwarding
→ succeeded
→ finalized
→ pending_charge
→ partially_charged
→ charged
```

Отдельные terminal/error states:

```text
rejected
forwarding_failed
pricing_failed
cancelled
```

Реализовать атомарные операции:

1. Создать usage record.
2. Проверить idempotency scope.
3. Зарезервировать estimated user amount.
4. Зарезервировать estimated reseller cost.
5. Зафиксировать выбранный route.
6. Зафиксировать forwarding attempt.
7. Сохранить actual или estimated usage.
8. Рассчитать final client amount.
9. Скорректировать reservation.
10. Освободить неиспользованный reseller reserve.
11. Перевести запись в pending charge.
12. Применить billing allocation.

Использовать:

```text
SELECT ... FOR UPDATE;
transaction boundaries;
optimistic version/state checks;
unique idempotency constraints;
atomic balance adjustments.
```

### Обязательное исправление partial charge

`BillingChargeRequestID` нельзя устанавливать всем records группы.

Claim получают только records и суммы, реально вошедшие в:

```text
billing_charge_allocations
```

Если запись списана частично:

```text
remaining_amount_cents > 0
```

она должна снова участвовать в следующем charge batch.

### Результат

Ни один сбой процесса не создаёт:

* потерянный usage;
* двойное списание;
* отрицательный незарезервированный баланс;
* навсегда зависший partially charged record.

---

## Этап 5. Завершить auto-charge

Исправить `AutoChargeService.Run()` так, чтобы один run обрабатывал все группы:

```text
provider_type + client_model
```

Для каждой группы:

1. загрузить eligible records;
2. сформировать отдельный charge plan;
3. создать billing charge request;
4. вызвать Billing Service;
5. применить allocations;
6. сохранить остатки;
7. перейти к следующей группе.

Нельзя возвращаться после первой успешно найденной группы.

Реализовать:

* threshold trigger после finalization;
* периодический worker для pending records;
* retry failed batch;
* idempotency key для Billing Service;
* exponential backoff;
* max attempts;
* reconciliation неопределённых результатов;
* отдельные состояния timeout и definite rejection;
* dead-letter/admin resolution после исчерпания retry.

### Результат

Pending usage гарантированно подхватывается повторно, даже если запрос, запустивший threshold, завершился или процесс упал.

---

## Этап 6. Создать provider adapter registry

Generic orchestration не должен выбирать adapter через `switch provider_type`.

Создать registry с ключом:

```text
api_family + provider_type
```

Descriptor должен содержать:

```text
forwarder;
usage extractor;
error classifier;
token estimator;
model rewrite capability;
authorization strategy;
safe header policy.
```

Первой зарегистрировать OpenAI-compatible группу:

```text
openai;
openrouter;
together;
groq;
hydra;
vllm;
lmstudio.
```

Это могут быть разные descriptors поверх общего OpenAI-compatible transport, но provider-specific:

```text
authorization;
headers;
usage shape;
error classification;
base URL rules
```

должны находиться в adapter infrastructure.

Неизвестная комбинация должна возвращать deterministic:

```text
adapter_not_available
```

а не выбирать ближайший adapter.

### Результат

Новый reseller/provider добавляется регистрацией adapter descriptor и metadata, без изменений generic request pipeline.

---

## Этап 7. Реализовать первый сквозной vertical slice

Первый полностью рабочий endpoint:

```text
POST /v1/chat/completions
```

### Порядок обработки

#### 7.1. Transport boundary

Handler выполняет только:

1. создаёт `llmreq_<random>`;
2. устанавливает `X-Local-Request-ID`;
3. проверяет HTTP method;
4. проверяет auth header syntax;
5. применяет body size limit;
6. читает body;
7. вызывает `llmrequest.Execute`;
8. преобразует application error в HTTP response;
9. записывает upstream status, headers и body.

#### 7.2. Authentication

Application use case:

1. HMAC-SHA256 raw key;
2. lookup API key;
3. проверка enabled/revoked/expired;
4. проверка user enabled;
5. обновление `last_used_at`;
6. получение `billing_subject_user_id`.

#### 7.3. Structural parsing

Parser обязан:

* сохранить byte-for-byte копию body;
* отклонить invalid UTF-8;
* отклонить duplicate JSON keys;
* отклонить trailing JSON;
* ограничить nesting depth;
* извлечь `model`;
* проверить `stream`;
* определить capabilities только структурно.

#### 7.4. Billing admission

Проверить:

```text
wallet balance;
pending amount;
reserved amount;
effective balance.
```

Получение пользовательского баланса должно использовать Billing JWT, а списание — отдельный service token.

#### 7.5. Route candidates

Загрузить routes только по:

```text
api_family = openai_compatible
endpoint_kind = chat
client_model = requested model
```

Отфильтровать:

```text
disabled route;
disabled reseller;
cooldown;
missing reseller credential;
unsupported rewrite;
missing/disabled price;
currency mismatch;
insufficient reseller balance;
missing capability.
```

#### 7.6. Preflight

Для каждого кандидата:

* оценить usage;
* применить safety coefficient;
* вычислить estimated upstream cost;
* вычислить estimated client amount;
* проверить route limits;
* проверить user effective balance;
* проверить reseller available balance.

#### 7.7. Route selection

Порядок:

```text
estimated sell/upstream cost;
priority;
route_id.
```

Выбор должен быть детерминированным.

#### 7.8. Ledger reservation

В одной транзакции:

* создать или получить idempotent usage record;
* записать route;
* зарезервировать client amount;
* зарезервировать reseller amount;
* перевести record в forwarding state.

#### 7.9. Forwarding

Adapter:

* получает reseller secret через `SecretResolver`;
* удаляет hop-by-hop headers;
* не передаёт client Authorization;
* устанавливает reseller credential;
* сохраняет original body;
* при разрешённой policy меняет только `body.model`;
* вызывает upstream с timeout;
* возвращает upstream status, headers и body.

#### 7.10. Usage и pricing

При successful upstream response:

1. provider usage extractor;
2. если usage отсутствует — conservative estimator;
3. normalization;
4. final price calculation;
5. ledger finalization;
6. reservation reconciliation;
7. auto-charge trigger.

Если upstream success, но pricing не выполнен:

```text
usage_record = pricing_failed;
response body всё равно возвращается;
X-Billing-Status: pricing_failed;
автоматическое списание не выполняется;
record доступен admin resolution.
```

#### 7.11. Response

Клиент получает неизменённые:

```text
status code;
response body;
безопасные upstream headers.
```

Gateway добавляет:

```text
X-Local-Request-ID;
X-Billing-Status;
X-Billing-Provider-Type;
X-Billing-Client-Model;
X-Billing-Model;
usage headers;
amount headers;
wallet/pending headers.
```

### Результат

Через один реальный reseller проходит полноценный платный chat request от user API key до Billing Service.

---

## Этап 8. Реализовать fallback, retry и cooldown

Fallback разрешается только между routes с одинаковыми:

```text
api_family;
endpoint_kind;
client_model;
requested capabilities.
```

Adapter error classifier должен разделять:

```text
client error — retry запрещён;
authentication/config error — route disabled or long cooldown;
rate limit — retry другого route;
temporary upstream error — retry;
timeout before confirmed send — retry policy;
unknown completion state — отдельная reconciliation policy.
```

Для каждой попытки сохранять `route_event`.

При неуспешной route:

* освобождать её reservation;
* ставить cooldown при необходимости;
* резервировать следующий route;
* не создавать второй billable usage record;
* использовать тот же `local_request_id`.

Ограничить:

```text
max route attempts;
общий request deadline;
per-attempt timeout.
```

### Результат

Fallback не приводит к двойному billing и никогда не меняет API family.

---

## Этап 9. Реализовать idempotency полностью

Scope:

```text
user_id + endpoint_kind + idempotency_key
```

Добавить request fingerprint, чтобы один ключ нельзя было использовать с другим body.

Состояния повторного запроса:

```text
processing       → deterministic conflict/in-progress;
completed        → replay согласно принятой policy;
failed_retryable → контролируемый повтор;
failed_final     → сохранённая terminal ошибка.
```

Перед реализацией явно закрепить в спецификации, хранится ли полный upstream response для replay.

Практичный production-вариант:

* ограничивать размер сохраняемого response;
* шифровать или не сохранять body в зависимости от privacy policy;
* при отсутствии body возвращать deterministic replay metadata;
* никогда не создавать второе списание.

### Результат

Повторный клиентский retry не создаёт второй usage и второй billing charge.

---

## Этап 10. Подключить остальные OpenAI-compatible endpoints

После стабильного chat pipeline подключить к тому же orchestrator:

```text
POST /v1/embeddings
POST /v1/images/generations
```

Различаться должны только endpoint descriptors:

```text
endpoint_kind;
base capability;
parser;
usage extractor;
pricing unit;
upstream path.
```

### Embeddings

Проверить:

```text
capability embeddings;
input token usage;
input-only pricing;
model rewrite;
response passthrough.
```

### Images generation

Проверить:

```text
capability images_generation;
image_generation_units;
provider usage или conservative estimation;
per-image pricing;
response passthrough.
```

Нельзя копировать chat handler и создавать отдельный billing pipeline.

### Результат

Полностью реализован обязательный `/v1/*` surface первой версии.

---

## Этап 11. Завершить pricing и usage

Добавить полный набор unit tests для:

```text
text input;
cached input;
output;
reasoning;
image input;
audio input/output;
file input;
video input;
image generation units;
zero usage;
missing usage;
multimodal max-input rule;
markup;
single final rounding;
currency validation;
preflight safety coefficient;
default_max_output_tokens.
```

Правила:

* денежные вычисления не выполнять через прямой `float64`;
* markup преобразовывать через canonical decimal;
* округлять один раз в конце;
* upstream cost хранить отдельно от client amount;
* estimated usage никогда не должен случайно давать нулевой billing.

### Результат

Pricing детерминирован и одинаков в catalog, preflight и finalization.

---

## Этап 12. Завершить Admin API

Проверить и добавить недостающие операции:

### Users и keys

```text
create/disable/enable user;
create/revoke key;
list keys без raw value;
изменение billing identity.
```

### Resellers

```text
create/update/disable;
api_key_env validation;
manual balance correction с reason;
credential presence diagnostic без раскрытия секрета.
```

### Routes и prices

```text
create/update/disable;
capabilities;
priority;
model rewrite policy;
cooldown set/clear;
price history;
currency validation.
```

### Usage и billing

```text
list usage;
list pricing_failed;
resolve pricing_failed;
list charge batches;
retry failed batch;
просмотр allocations;
просмотр partial charge remainder.
```

### Provisioning

```text
metadata без encrypted/raw key;
status;
expiration;
delivery confirmation.
```

### Audit

Все опасные операции сохраняют:

```text
admin identity;
action;
target;
old value;
new value;
reason;
timestamp;
request id.
```

Добавить pagination ко всем list endpoints.

### Результат

Admin API удовлетворяет всем acceptance criteria из спецификации, включая audit, manual resolution и secrets policy.

---

## Этап 13. Довести provisioning до production-ready

Проверить полный платёжный flow:

```text
trusted caller
→ POST /internal/v1/api-keys/provision
→ service token auth
→ create or return same provisioning
→ same raw key on retry
→ delivery confirmation
→ encrypted material deletion.
```

Реализовать:

* уникальность `idempotency_key`;
* same-key retry;
* authenticated encryption;
* key rotation strategy;
* expiration worker;
* atomic permanent key creation;
* delivery confirmation;
* очистку encrypted material;
* запрет повторного получения после confirmation;
* audit metadata.

Интеграционный тест должен симулировать потерю первого HTTP response и повторный вызов.

### Результат

Оплаченный пользователь гарантированно получает ровно один API key даже при сетевых retries.

---

## Этап 14. Реализовать native API families

Только после завершения OpenAI-compatible pipeline.

### Anthropic-native

```text
POST /v1/messages
```

Добавить:

* отдельный structural parser;
* `body.model`;
* Anthropic auth/header adapter;
* Anthropic usage extractor;
* error classifier;
* response passthrough.

### Gemini-native

```text
POST /v1beta/models/{model}:generateContent
POST /v1beta/models/{model}:embedContent
POST /v1beta/models/{model}:batchEmbedContents
GET  /v1beta/models
```

Model извлекать из path.

`streamGenerateContent` до поддержки streaming должен возвращать:

```text
streaming_unsupported
```

### Ollama-native

```text
POST /api/chat
POST /api/generate
POST /api/embeddings
GET  /api/tags
```

Для каждой family:

* свой parser;
* свой adapter;
* свой usage extractor;
* тот же generic ledger/pricing/billing orchestration;
* никакой конвертации в OpenAI body.

### Результат

Клиент каждого SDK меняет только `base_url` и credential, сохраняя родной API format.

---

## Этап 15. Добавить background workers

Нужны отдельные workers:

```text
auto-charge threshold/periodic worker;
failed billing batch retry;
billing reconciliation;
provisioning expiration;
route cooldown recovery;
Telegram reseller balance alerts.
```

Каждый worker должен иметь:

* distributed lock или безопасную конкурентную выборку;
* batch size;
* polling interval;
* graceful shutdown;
* retry policy;
* structured logs;
* метрики;
* состояние последнего успешного прохода.

Public `/billing/flush` добавлять нельзя.

### Результат

Финансовый lifecycle не зависит от активности входящих пользовательских запросов.

---

## Этап 16. Реализовать limits и resource safety

Добавить:

```text
request body limit;
upstream response limit;
HTTP server timeouts;
upstream connect/header/body timeouts;
DB pool limits;
per-user request limit;
per-key request limit;
per-route concurrency limit;
reseller rate limit;
global concurrency limit;
max fallback attempts.
```

Rate/concurrency capacity должна проверяться непосредственно перед forwarding, а не только при построении catalog.

При отказе после reservation ресурс и reservation должны освобождаться.

Добавить graceful shutdown:

1. перестать принимать новые requests;
2. дождаться активных requests;
3. остановить workers;
4. закрыть HTTP clients;
5. закрыть DB pool.

### Результат

Gateway не истощает память, goroutines, DB connections и reseller limits под нагрузкой.

---

## Этап 17. Закрыть security boundary

Проверить:

* HMAC secret обязателен;
* fallback на обычный SHA-256 отсутствует;
* service tokens сравниваются constant-time;
* billing JWT имеет короткий TTL, `iss`, `aud`;
* raw keys не логируются;
* Authorization удаляется из logs;
* request/response body по умолчанию не логируются;
* `api_key_env` проходит строгую validation;
* reseller URL проходит URL/SSRF validation;
* HTTPS обязателен в production;
* redirects upstream запрещены или строго контролируются;
* internal/admin endpoints отделены сетевой политикой;
* secrets не возвращаются в diagnostics;
* SQL queries параметризованы;
* encryption key rotation документирована.

### Результат

Все trust boundaries из auth specification enforce-ятся кодом, а не договорённостью.

---

## Этап 18. Добавить observability

### Structured logs

Обязательные поля:

```text
request_id;
user_id;
api_key_id;
api_family;
endpoint_kind;
client_model;
route_id;
reseller_id;
attempt;
ledger_state;
billing_batch_id;
duration;
error_code.
```

Не логировать:

```text
raw API keys;
billing JWT;
reseller credentials;
prompt body;
upstream response body.
```

### Metrics

Добавить:

```text
requests_total;
request_duration;
upstream_duration;
route_attempts;
route_failures;
route_cooldowns;
usage_amount_cents;
pending_charge_cents;
billing_batches;
billing_failures;
pricing_failures;
active_requests;
DB pool metrics;
worker lag.
```

### Health endpoints

Разделить:

```text
/health  — процесс жив;
/ready   — DB и обязательные dependencies доступны.
```

### Tracing

Добавить OpenTelemetry spans:

```text
authenticate;
admission;
route_lookup;
preflight;
reserve;
forward_attempt;
usage_extract;
pricing;
finalize;
auto_charge.
```

Без записи semantic payload.

### Результат

Любой финансовый и routing сбой можно восстановить по request ID и persisted state.

---

## Этап 19. Построить тестовую пирамиду

### Unit tests

Покрыть:

```text
HMAC auth;
structural parsers;
duplicate JSON keys;
capability detection;
route selector;
pricing;
ledger state machine;
billing plans;
partial charge;
error mapping;
model rewrite;
header filtering.
```

### Postgres integration tests

Через реальный Postgres:

```text
migrations;
transactions;
concurrent reservation;
idempotency;
partial allocations;
worker locking;
admin audit;
provisioning retry.
```

### Contract tests

Поднять fake services:

```text
fake Billing Service;
fake OpenAI-compatible reseller;
fake Anthropic;
fake Gemini;
fake Ollama.
```

Проверить:

* request body passthrough;
* только model rewrite;
* response body passthrough;
* auth replacement;
* billing headers;
* retry;
* timeout;
* error classification.

### End-to-end tests

Проверить реальными SDK:

```text
OpenAI SDK;
Anthropic SDK;
Gemini SDK;
Ollama client.
```

### Fuzz tests

Минимум:

```text
JSON structural parser;
duplicate-key detector;
model rewrite;
native path parser;
header filtering.
```

### Race и нагрузка

```bash
go test -race ./...
```

Проверить:

* параллельные одинаковые idempotency keys;
* одновременный auto-charge;
* несколько gateway instances;
* route balance contention;
* shutdown во время forwarding.

### Результат

Каждому acceptance criterion в `docs` соответствует автоматический тест.

---

## Этап 20. Создать CI/CD и runtime packaging

Добавить:

```text
Dockerfile;
docker-compose.dev.yml;
Makefile targets;
.env.example без secrets;
GitHub Actions;
migration command;
release build.
```

CI pipeline:

```text
gofmt check;
go vet;
staticcheck;
unit tests;
race tests;
Postgres integration tests;
migration smoke test;
build;
container scan;
dependency vulnerability scan.
```

Production startup:

1. validate config;
2. проверить наличие обязательных secrets;
3. применить migrations отдельным release job;
4. запустить gateway;
5. дождаться readiness;
6. включить traffic.

Добавить:

* non-root container;
* read-only filesystem, где возможно;
* resource limits;
* graceful termination;
* backup/restore policy для Postgres;
* rollback procedure;
* secret rotation procedure.

### Результат

Один и тот же artifact проходит dev, staging и production.

---

# Порядок релизных milestones

## Milestone A — собираемое ядро

Готово:

```text
migrations;
build;
layer boundaries;
unit test foundation.
```

Критерий:

```bash
go test ./...
go test -race ./...
go build ./cmd/gateway
```

---

## Milestone B — internal alpha

Готово:

```text
POST /v1/chat/completions;
один OpenAI-compatible reseller;
auth;
route selection;
ledger;
pricing;
billing;
auto-charge;
billing headers.
```

Критерий:

Один реальный запрос проходит полный financial lifecycle без ручного изменения БД.

---

## Milestone C — public API beta

Готово:

```text
/v1/models;
/v1/chat/completions;
/v1/embeddings;
/v1/images/generations;
fallback;
cooldown;
idempotency;
несколько resellers;
admin API.
```

Критерий:

Gateway работает как OpenAI-compatible base URL с несколькими routes и корректным billing.

---

## Milestone D — release candidate

Готово:

```text
Anthropic native;
Gemini native;
Ollama native;
workers;
Telegram alerts;
limits;
observability;
security hardening;
load tests.
```

Критерий:

Все acceptance criteria из `docs/spec` покрыты тестами.

---

## Milestone E — 1.0.0

Готово:

```text
CI/CD;
staging;
production migrations;
backup/restore;
alerts;
runbooks;
incident recovery;
key rotation;
billing reconciliation;
zero unresolved critical defects.
```

# Рекомендуемая последовательность PR

```text
PR-01  Добавить SQL migrations и migration tests
PR-02  Исправить application dependency boundaries
PR-03  Закончить ledger transactions и state machine
PR-04  Исправить partial charge claiming
PR-05  Исправить обработку всех auto-charge groups
PR-06  Добавить adapter registry
PR-07  Добавить llmrequest orchestrator
PR-08  Реализовать POST /v1/chat/completions transport
PR-09  Подключить forwarding и response passthrough
PR-10  Подключить usage, pricing и finalization
PR-11  Подключить auto-charge trigger и worker
PR-12  Реализовать fallback/cooldown
PR-13  Реализовать idempotency
PR-14  Добавить embeddings
PR-15  Добавить images generation
PR-16  Завершить Admin API
PR-17  Завершить provisioning lifecycle
PR-18  Добавить Anthropic-native
PR-19  Добавить Gemini-native
PR-20  Добавить Ollama-native
PR-21  Добавить rate/concurrency limits
PR-22  Добавить Telegram alerts и workers
PR-23  Добавить observability и security hardening
PR-24  Добавить E2E/load/chaos tests
PR-25  Добавить Docker, CI/CD и production runbooks
```
