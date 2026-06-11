# 030. Routing and Resellers

Версия: 0.1
Статус: draft
Язык: русский
Проект: `github.com/bogachenko/tokenio-gateway`

---

# 1. Назначение документа

Этот документ описывает routing layer Tokenio Gateway.

Документ фиксирует:

```text
provider_type
reseller
route
api_family
endpoint_kind
client_model
provider_model
capabilities
route availability
route selection algorithm
cooldown
retry between routes
reseller balance accounting
rate limits
fallback rules
```

Документ не описывает:

```text
public HTTP API
API key validation
billing JWT
final token pricing formula
ledger state machine
admin API endpoints
database migrations
```

Эти темы описываются в отдельных спецификациях.

---

# 2. Главный routing invariant

Route selection не должен выполняться только по `model`.

Правильный routing key:

```text
api_family + endpoint_kind + client_model
```

Fallback разрешён только между routes с одинаковыми:

```text
api_family
endpoint_kind
client_model
```

Запрещено:

```text
OpenAI-compatible request -> Gemini-native route
Gemini-native request -> OpenAI-compatible route
Anthropic-native request -> OpenAI-compatible route
Ollama-native request -> OpenAI-compatible route
```

Gateway не конвертирует semantic request payload между API families; explicit model identifier rewrite не меняет API family.

---

# 3. Provider type

`provider_type` — тип upstream/provider ecosystem.

Поддерживаемые значения:

```text
openai
openrouter
together
groq
ollama
lmstudio
vllm
gemini
anthropic
hydra
```

`provider_type` не является reseller.

Пример:

```text
provider_type = openrouter
```

означает класс upstream/provider, но не конкретный аккаунт.

---

# 4. Reseller

## 4.1. Определение

Reseller — конкретный upstream account/base_url/API key/balance/limits.

Reseller содержит:

```text
id
name
provider_type
base_url
api_key_env
enabled
balance_cents
reserved_cents
minimum_balance_cents
created_at
updated_at
```

## 4.2. Reseller API key

Reseller API key хранится не в БД, а в environment variable.

В БД хранится только имя env-переменной:

```text
api_key_env
```

Пример:

```text
api_key_env = OPENROUTER_PRIMARY_API_KEY
```

Gateway читает секрет так:

```text
os.Getenv(route.reseller.api_key_env)
```

Если env отсутствует или пустой, все routes этого reseller считаются unavailable.

Причина unavailable:

```text
missing_reseller_api_key
```

Обычному client response нельзя раскрывать имя env-переменной.

## 4.3. Reseller enabled flag

Если:

```text
reseller.enabled = false
```

все routes reseller считаются unavailable.

Причина:

```text
manual_disabled
```

## 4.4. Reseller balance

Gateway хранит расчётный баланс reseller в RUB cents:

```text
balance_cents
reserved_cents
minimum_balance_cents
```

`balance_cents` — расчётный остаток средств у reseller.
`reserved_cents` — сумма preflight reserves по in-flight запросам.
`minimum_balance_cents` — минимальный остаток, ниже которого reseller не должен использоваться.

Доступный баланс reseller:

```text
available_reseller_balance_cents =
  balance_cents - reserved_cents - minimum_balance_cents
```

Если:

```text
available_reseller_balance_cents < estimated_upstream_cost_cents
```

route недоступен.

Причина:

```text
insufficient_reseller_balance
```

---

# 5. Route

## 5.1. Определение

Route — возможность купить конкретную client model у конкретного reseller через конкретный API family и endpoint kind.

Route содержит:

```text
id
reseller_id
provider_type
api_family
endpoint_kind
client_model
provider_model
model_rewrite_policy
enabled
priority
requests_per_minute
tokens_per_minute
concurrent_requests
default_max_output_tokens
capabilities
cooldown_until
cooldown_reason
created_at
updated_at
```

## 5.2. Route identity

Route identity должна быть стабильной.

Route ID используется в:

```text
usage ledger
reseller balance reserve
debug/admin API
cooldown
logs
metrics
```

Route ID не должен быть публичным client-facing identifier.

## 5.3. Client model

`client_model` — имя модели, которое видит клиент.

Пример:

```text
gpt-4.1-mini
gemini-2.5-flash
claude-3-5-sonnet
```

Client request использует именно `client_model`.

## 5.4. Provider model

`provider_model` — имя модели, которое нужно отправить upstream reseller.

Пример:

```text
client_model: gpt-4.1-mini
provider_model: openai/gpt-4.1-mini
```

Если API family и reseller позволяют использовать client model без изменения, тогда:

```text
provider_model = client_model
model_rewrite_policy = none
```

Если upstream reseller требует другое имя модели, route обязан явно включить:

```text
model_rewrite_policy = provider_model
```

В этом случае provider adapter может заменить только model identifier.

Для OpenAI-compatible requests разрешённая mutation ограничена:

```text
body.model = route.provider_model
```

Для path-based native APIs разрешённая mutation ограничена model path segment, если adapter явно поддерживает эту операцию.

Это не является API-format conversion.

Запрещено вместе с model rewrite:

```text
конвертировать messages/content/tools
конвертировать multimodal payload
конвертировать response_format
менять semantic request payload
делать fallback в другую API family
```

Если `model_rewrite_policy = provider_model`, но adapter не поддерживает безопасную model rewrite для данного API family, route считается invalid и не должен выбираться.

Model rewrite route selection rule:

```text
routes requiring model_rewrite_policy = provider_model are eligible only if adapter supports safe model identifier rewrite for this api_family.
```

If adapter does not support safe model rewrite:

```text
route is treated as unavailable
reason = unsupported_model_rewrite_policy
```




## 5.5. Model rewrite policy

Allowed values:

```text
none
provider_model
```

`none` означает:

```text
client model identifier отправляется upstream без изменения
```

`provider_model` означает:

```text
adapter заменяет только model identifier на route.provider_model
```

Route validation:

```text
if provider_model != client_model, model_rewrite_policy must be provider_model
if model_rewrite_policy = provider_model, provider_model must be non-empty
if model_rewrite_policy = none, provider_model should equal client_model unless adapter explicitly documents otherwise
```


---

# 6. API family

## 6.1. Поддерживаемые API families

Минимальный набор:

```text
openai_compatible
gemini_native
anthropic_native
ollama_native
```

Первая публичная `/v1/*` surface относится к:

```text
openai_compatible
```

## 6.2. API family detection

Gateway определяет `api_family` по endpoint path.

Первая версия:

```text
/v1/chat/completions    -> openai_compatible
/v1/embeddings          -> openai_compatible
/v1/images/generations  -> openai_compatible
/v1/models              -> openai_compatible
```

Native paths для Gemini/Anthropic/Ollama описаны в `docs/spec/011-native-api-families.ru.md`.

## 6.3. Fallback boundary

Fallback между routes возможен только внутри одного API family.

Запрещено fallback-ить между:

```text
openai_compatible <-> gemini_native
openai_compatible <-> anthropic_native
openai_compatible <-> ollama_native
gemini_native <-> anthropic_native
```

---

# 7. Endpoint kind

Поддерживаемые endpoint kinds первой версии:

```text
chat
embeddings
images_generation
models
health
```

Routing применяется только к LLM upstream endpoints:

```text
chat
embeddings
images_generation
```

`models` строится из registry/routes, но не forward-ится как обычный LLM request.

`health` не использует routing.

---

# 8. Capabilities

## 8.1. Capability set

Route должен объявлять capabilities.

Минимальный набор:

```text
chat
embeddings
images_generation
tools
tool_choice
response_format
json_schema
image_input
audio_input
file_input
video_input
reasoning
```

## 8.2. Capability validation

Gateway должен определить requested capabilities структурно из request body.

Запрещено определять capabilities по смыслу prompt text.

Примеры structural detection:

```text
tools present             -> tools
tool_choice present       -> tool_choice
response_format present   -> response_format
response_format json_schema -> json_schema
image content present     -> image_input
audio content present     -> audio_input
file/document present     -> file_input
video content present     -> video_input
reasoning_effort present  -> reasoning
```

Если request требует capability, которой нет у route, route исключается из candidate set.

Если model существует, но нет route с нужными capabilities:

```text
HTTP 400
error.code = unsupported_capability
```

---

# 9. Route price

## 9.1. Route-specific price

Цена задаётся на уровне route.

Route price содержит:

```text
route_id
currency
input_price_per_1m_tokens_cents
cached_input_price_per_1m_tokens_cents
output_price_per_1m_tokens_cents
reasoning_output_price_per_1m_tokens_cents
image_input_price_per_1m_tokens_cents
audio_input_price_per_1m_tokens_cents
audio_output_price_per_1m_tokens_cents
file_input_price_per_1m_tokens_cents
video_input_price_per_1m_tokens_cents
image_generation_price_per_unit_cents
image_generation_unit_kind
markup_coefficient
```

Currency первой версии:

```text
RUB
```

## 9.2. Selection cost

Route selection сортирует candidates по estimated total upstream cost, а не по одной input price.

Estimated total upstream cost рассчитывается до markup.

Client sell price рассчитывается отдельно pricing layer.

## 9.3. Markup

Markup не влияет на выбор самого дешёвого upstream route, если markup одинаковый.

Если markup задан per route, route selection для клиента может учитывать estimated client amount.

Решение первой версии:

```text
route selection сортирует по estimated_upstream_cost_cents
billing для клиента считает selected route price * markup
```

---

# 10. Route availability

Route считается available, если выполняются все условия:

```text
route.enabled = true
reseller.enabled = true
reseller api key env exists and non-empty
route cooldown is absent or expired
required capabilities are supported
estimated upstream cost <= available reseller balance
route rate/concurrency limits allow request
route model_rewrite_policy is supported by adapter
```

Если хотя бы одно условие не выполнено, route исключается из candidates.

---

# 11. Route selection algorithm

## 11.1. Inputs

Route selector получает:

```text
user_id
api_family
endpoint_kind
client_model
requested_capabilities
estimated_usage
request_id
idempotency_key
now
```

## 11.2. Algorithm

Route selector должен выполнить:

```text
1. Найти enabled routes для:
   api_family + endpoint_kind + client_model

2. Если routes не найдены:
   return unknown_model

3. Отфильтровать routes, чей reseller disabled.

4. Отфильтровать routes с missing reseller API key.

5. Отфильтровать routes в active cooldown.

6. Отфильтровать routes без required capabilities.

7. Рассчитать estimated_upstream_cost_cents для каждого candidate.

8. Отфильтровать routes, где:
   available_reseller_balance_cents < estimated_upstream_cost_cents

9. Отфильтровать routes по локальным rate/concurrency limits.

10. Отфильтровать routes, где:
    model_rewrite_policy требует rewrite,
    но adapter не поддерживает safe model identifier rewrite для этого api_family.

11. Отсортировать candidates по:
    estimated_upstream_cost_cents ASC
    priority ASC
    route_id ASC

12. Выбрать первый candidate.

13. Зарезервировать estimated_upstream_cost_cents на reseller.

14. Вернуть selected route.
```

## 11.3. Unknown model

Если route registry не содержит ни одного route для:

```text
api_family + endpoint_kind + client_model
```

ответ:

```text
HTTP 400
error.code = unknown_model
```

## 11.4. Unsupported capability

Если routes существуют, но все отфильтрованы из-за missing capabilities:

```text
HTTP 400
error.code = unsupported_capability
```

## 11.5. No route available

Если routes существуют, но все недоступны из-за balance/cooldown/rate limit/missing key/disabled:

```text
HTTP 503
error.code = no_route_available
```

---

# 12. Reseller balance reserve

## 12.1. Preflight reserve

Перед отправкой upstream request gateway должен зарезервировать estimated upstream cost на reseller:

```text
reseller.reserved_cents += estimated_upstream_cost_cents
```

Reserve нужен, чтобы один instance не отправил слишком много одновременных запросов в reseller, у которого расчётный баланс уже почти исчерпан.

## 12.2. Release reserve on safe failure

Если request не был принят upstream или точно не начал обрабатываться, reserve освобождается:

```text
reseller.reserved_cents -= estimated_upstream_cost_cents
```

Safe failure examples:

```text
connection refused before response headers
DNS error
TLS handshake error
local timeout before request write completed
missing reseller API key
route selection failure before upstream
```

## 12.3. Reconcile reserve on success

После успешного upstream response gateway должен:

```text
reseller.reserved_cents -= estimated_upstream_cost_cents
reseller.balance_cents -= actual_upstream_cost_cents
```

Если actual upstream cost неизвестен, используется conservative calculated cost.

## 12.4. Actual > estimated

Если actual upstream cost больше estimated:

```text
reseller.balance_cents -= actual_upstream_cost_cents
```

Даже если reseller balance станет отрицательным, ledger должен отразить фактический расход.

Estimator должен быть conservative, чтобы такое происходило редко.

---

# 13. Retry between routes

## 13.1. Retry boundary

Retry на следующий route разрешён только если:

```text
upstream точно не начал обработку request
или ошибка классифицирована как safe retryable before-response error
```

Retry запрещён, если есть риск двойной обработки upstream.

## 13.2. Retryable route failures

Route может быть исключён и следующий route попробован при:

```text
rate_limit
quota_exceeded
insufficient_reseller_balance
provider_5xx before response body is committed
timeout before response headers
connection_error before response headers
```

## 13.3. Non-retryable failures

Retry запрещён при:

```text
upstream returned successful HTTP status
upstream returned deterministic 4xx caused by client request
response headers already received and body passthrough started
request may have been processed by upstream
```

## 13.4. Same-family retry only

Retry разрешён только внутри того же:

```text
api_family
endpoint_kind
client_model
```

---

# 14. Cooldown

## 14.1. Cooldown reasons

Route может быть переведён в cooldown по причинам:

```text
rate_limit
quota_exceeded
insufficient_reseller_balance
provider_5xx
timeout
manual_disabled
healthcheck_failed
missing_reseller_api_key
auth_error
```

## 14.2. Cooldown durations

Cooldown durations задаются через config.

Рекомендуемые defaults:

```text
TOKENIO_ROUTE_COOLDOWN_RATE_LIMIT=60s
TOKENIO_ROUTE_COOLDOWN_QUOTA_EXCEEDED=24h
TOKENIO_ROUTE_COOLDOWN_5XX=30s
TOKENIO_ROUTE_COOLDOWN_TIMEOUT=30s
TOKENIO_ROUTE_COOLDOWN_AUTH_ERROR=24h
```

## 14.3. Manual disabled

Если route или reseller отключён вручную, это не cooldown.

Это persistent disabled state.

## 14.4. Cooldown visibility

Обычный client response не должен раскрывать detailed cooldown internals.

Admin API может показывать:

```text
route_id
cooldown_until
cooldown_reason
last_error_code
last_error_at
```
## 14.5. Route skip reasons

Route skip reasons are diagnostic values for route selection and admin/debug visibility.

They are not necessarily cooldown reasons.

Minimum route skip reasons:

```text
missing_reseller_api_key
manual_disabled
cooldown_active
missing_capability
insufficient_reseller_balance
rate_limit_exceeded
concurrency_limit_exceeded
unsupported_model_rewrite_policy
invalid_route_price
pricing_unavailable
```

`unsupported_model_rewrite_policy` means:

```text
route requires model_rewrite_policy = provider_model,
but the selected provider adapter cannot safely rewrite only model identifier for this api_family.
```

This is a configuration/adapter compatibility issue, not a temporary upstream failure.


---

# 15. Rate limits

## 15.1. Route-level limits

Route может иметь limits:

```text
requests_per_minute
tokens_per_minute
concurrent_requests
```

Если значение равно `0`, limit считается disabled.

## 15.2. One instance assumption

Первая версия работает в режиме:

```text
single instance
```

Поэтому in-memory limiter допустим для route-level request pacing.

Но reseller balance reserve должен храниться в Postgres, потому что это source of truth для accounting.

## 15.3. Rate limit exceeded

Если local route limit exceeded, route временно исключается из candidates.

Если все routes исключены:

```text
HTTP 503
error.code = no_route_available
```

---

# 16. Provider adapter boundary

Provider-specific logic не должна попадать в generic route selector.

Provider-specific behavior живёт в adapters:

```text
forwarding adapter
usage extractor
error classifier
token estimator
capability mapper
```

Generic route selector не должен знать:

```text
как именно OpenRouter возвращает ошибку
как именно Gemini считает usage
как именно Anthropic называет поля
как именно Hydra возвращает cost_request
```

Route selector работает только с нормализованными результатами adapter layer.

---

# 17. Models endpoint interaction

`GET /v1/models` строится из route registry.

Для каждой `client_model` gateway должен определить public availability.

Если есть хотя бы один available route:

```text
active = true
pricing = cheapest available route sell price
capabilities = intersection of available routes for this client_model and endpoint type
```

Решение первой версии для capabilities:

```text
capabilities = intersection of available routes for this client_model and endpoint type
```


Причина:

```text
/v1/models не должен обещать combination capabilities,
если ни один concrete route не может обслужить такой request.
```

Будущая версия может вернуть `capability_profiles[]`, но первая версия использует conservative intersection.



Если нет available routes:

```text
active = false
```

`/v1/models` не раскрывает internal route/reseller data.

---

# 18. Telegram reseller balance alerts

Если reseller balance приближается к threshold:

```text
reseller.balance_cents - reseller.reserved_cents <= TOKENIO_RESELLER_BALANCE_ALERT_CENTS
```

gateway должен отправить Telegram alert.

Alert должен содержать:

```text
reseller_id
provider_type
available balance cents
threshold cents
time
```

Alert не должен содержать:

```text
reseller API key
api_key_env value
user API key
billing JWT
```

Alert deduplication описывается в configuration/operations spec.

---

# 19. Logging

Route selection может логировать:

```text
local_request_id
api_family
endpoint_kind
client_model
selected_route_id
selected_reseller_id
provider_type
estimated_upstream_cost_cents
cooldown_reason
route_selection_result
```

Запрещено логировать:

```text
raw request body
raw user API key
reseller API key
billing JWT
Authorization header
```

---

# 20. Acceptance criteria

Routing layer считается реализованным, если:

```text
1. Route lookup использует api_family + endpoint_kind + client_model.
2. Unknown model возвращает 400 unknown_model.
3. Missing capability возвращает 400 unsupported_capability.
4. No available route возвращает 503 no_route_available.
5. Cheapest available route выбирается по estimated upstream cost.
6. Route в cooldown не выбирается.
7. Disabled reseller не выбирается.
8. Route с missing api_key_env не выбирается.
9. Route с insufficient reseller balance не выбирается.
10. Перед upstream request создаётся reseller reserve.
11. Safe failure освобождает reserve.
12. Success reconciles reserve and actual upstream cost.
13. Retry не происходит между разными API families.
14. Retry не происходит после unsafe upstream processing.
15. Route with unsupported model_rewrite_policy is skipped before selection.
16. Generic selector не содержит provider-specific if/switch по конкретным APIs.
17. Unit tests покрывают route ordering, capability filtering, cooldown, balance filtering, missing env, unsupported_model_rewrite_policy, retry boundary и no_route_available.
```
