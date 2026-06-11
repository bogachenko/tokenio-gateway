# 010. External API

Версия: 0.1
Статус: draft
Язык: русский
Проект: `github.com/bogachenko/tokenio-gateway`

---

# 1. Назначение документа

Этот документ описывает публичный HTTP API Tokenio Gateway.

Документ фиксирует:

```text
base URL
public endpoints
authentication boundary
request passthrough rules
response passthrough rules
billing headers
idempotency behavior
public error envelope
```

Этот документ не описывает:

```text
database schema
admin API
route selection internals
pricing internals
billing ledger internals
provider adapter implementation
```

Для этих тем используются отдельные спецификации.

---

# 2. Общий внешний контракт

## 2.1. Base URL

Основной публичный base URL:

```text
https://<tokenio-domain>/v1
```

Клиентские SDK, агенты и приложения должны указывать этот URL как LLM base URL.

Пример:

```text
base_url = https://<tokenio-domain>/v1
api_key  = sk_...
```

## 2.2. Endpoint surface первой версии

Первая версия Tokenio Gateway обязана поддерживать:

```text
GET  /health
GET  /v1/models
POST /v1/chat/completions
POST /v1/embeddings
POST /v1/images/generations
```

## 2.3. API family первой версии

Публичные `/v1/*` endpoints первой версии относятся к:

```text
api_family = openai_compatible
```

Соответствие:

```text
/v1/chat/completions    -> endpoint_kind = chat
/v1/embeddings          -> endpoint_kind = embeddings
/v1/images/generations  -> endpoint_kind = images_generation
/v1/models              -> endpoint_kind = models
/health                 -> endpoint_kind = health
```

## 2.4. Request body passthrough

Tokenio Gateway не должен изменять request body клиента.

Запрещено:

```text
добавлять поля в body
удалять поля из body
переименовывать поля в body
конвертировать messages/content/tools
конвертировать multimodal payload
конвертировать response_format
конвертировать provider-specific options
```

Gateway может читать body только для:

```text
извлечения model
определения requested capabilities
preflight estimation
idempotency handling
audit-free local metadata
```

Прочитанный body должен быть отправлен upstream route без изменений.

## 2.5. Response body passthrough

Tokenio Gateway не должен изменять response body upstream.

Запрещено:

```text
добавлять billing поля в JSON body
удалять поля из JSON body
изменять формат ошибки upstream в успешном passthrough response
нормализовать successful response body
```

Billing metadata возвращается только через HTTP headers.

## 2.6. Header passthrough

Gateway может проксировать безопасные client headers upstream.

Gateway не должен проксировать upstream:

```text
Authorization
Host
Content-Length
Connection
Transfer-Encoding
Upgrade
Proxy-Authenticate
Proxy-Authorization
TE
Trailer
```

Gateway обязан заменить upstream authorization на reseller credential, полученный через `api_key_env`.

Gateway может проксировать:

```text
Content-Type
Accept
User-Agent
OpenAI-compatible optional headers, если они безопасны
Idempotency-Key, если route/provider это поддерживает
```

Provider-specific header policy описывается в provider adapter specification.

---

# 3. Authentication

## 3.1. Public auth scheme

Все публичные LLM endpoints, кроме `/health`, требуют user API key:

```http
Authorization: Bearer sk_...
```

User API key:

```text
не является JWT
не передаётся в billing service
не передаётся reseller
хранится только в hash form
```

## 3.2. Endpoints requiring auth

Требуют auth:

```text
GET  /v1/models
POST /v1/chat/completions
POST /v1/embeddings
POST /v1/images/generations
```

Не требует auth:

```text
GET /health
```

## 3.3. Auth errors

Если `Authorization` отсутствует:

```text
HTTP 401
error.code = unauthorized
```

Если `Authorization` не имеет формат `Bearer ...`:

```text
HTTP 401
error.code = unauthorized
```

Если bearer value не начинается с `sk_`:

```text
HTTP 401
error.code = unauthorized
```

Если API key не найден:

```text
HTTP 401
error.code = invalid_api_key
```

Если API key отключён или отозван:

```text
HTTP 401
error.code = invalid_api_key
```

Если пользователь отключён:

```text
HTTP 403
error.code = user_disabled
```

---

# 4. Request IDs

## 4.1. Local request id

Для каждого LLM-запроса Tokenio Gateway должен создать `local_request_id`.

Формат:

```text
llmreq_<random>
```

`local_request_id` используется для:

```text
usage ledger
idempotency
billing correlation
response headers
logs
debugging
```

## 4.2. Response header

Gateway должен возвращать:

```http
X-Local-Request-ID: llmreq_...
```

Header должен возвращаться для:

```text
успешных LLM responses
ошибок validation/auth/routing/billing
ошибок upstream forwarding, если request id уже создан
```

---

# 5. Idempotency-Key

## 5.1. Client-provided idempotency

Если клиент передал:

```http
Idempotency-Key: <value>
```

Tokenio Gateway должен использовать этот key для защиты от двойного списания.

Gateway не требует `Idempotency-Key`.

Если key не передан, gateway создаёт обычный `local_request_id`, но не гарантирует cross-request идемпотентность для повторного client retry.

## 5.2. Idempotency scope

Idempotency scope:

```text
user_id + endpoint_kind + idempotency_key
```

Один и тот же `Idempotency-Key` у разных пользователей не конфликтует.

Один и тот же `Idempotency-Key` на разных endpoint kinds не конфликтует.

## 5.3. Повторный запрос

Если запрос с тем же idempotency scope уже успешно завершён, gateway должен вернуть сохранённый result metadata и не создавать повторное billable списание.

Минимальное допустимое поведение первой версии:

```text
не делать повторный billing charge
не создавать второй billable usage
вернуть deterministic conflict/replay response согласно ledger state
```

Точная replay-стратегия response body описывается в ledger specification.

---

# 6. GET /health

## 6.1. Request

```http
GET /health
```

Auth не требуется.

## 6.2. Response

```http
200 OK
Content-Type: text/plain
```

Body:

```text
OK
```

## 6.3. Method restrictions

Если метод не `GET`:

```text
HTTP 405
error.code = method_not_allowed
```

---

# 7. GET /v1/models

## 7.1. Request

```http
GET /v1/models
Authorization: Bearer sk_...
```

## 7.2. Назначение

Вернуть список public client models, доступных через Tokenio Gateway.

`/v1/models` показывает client-facing model catalog, а не внутренние routes.

## 7.3. Response shape

Минимальный response:

```json
{
  "object": "list",
  "data": [
    {
      "id": "gpt-4.1-mini",
      "object": "model",
      "owned_by": "tokenio",
      "type": "chat",
      "active": true,
      "pricing": {
        "currency": "RUB",
        "input_price_per_1m_tokens_cents": 1000,
        "output_price_per_1m_tokens_cents": 4000
      },
      "capabilities": {
        "chat": true,
        "embeddings": false,
        "images_generation": false,
        "tools": true,
        "tool_choice": true,
        "response_format": true,
        "json_schema": true,
        "image_input": true,
        "audio_input": false,
        "file_input": false,
        "video_input": false,
        "reasoning": false
      }
    }
  ]
}
```

## 7.4. Pricing в /v1/models

Если одна client model доступна через несколько routes, public price должен рассчитываться по текущему самому дешёвому доступному route с учётом markup.

Если route в cooldown, он не считается доступным для public price.

Если все routes модели недоступны, модель может быть:

```text
скрыта из списка
или возвращена с active=false
```

Поведение должно быть единым и детерминированным.

Решение первой версии:

```text
если нет доступного route, модель возвращается с active=false
```

## 7.5. Запрещённые поля

`/v1/models` не должен раскрывать:

```text
reseller_id
route_id
api_key_env
provider API key
internal reseller balance
cooldown reason
private provider_model
internal priority
internal retry policy
```

---

# 8. POST /v1/chat/completions

## 8.1. Request

```http
POST /v1/chat/completions
Authorization: Bearer sk_...
Content-Type: application/json
```

Body — OpenAI-compatible chat completions body.

Tokenio Gateway не валидирует всю OpenAI schema. Gateway валидирует только поля, необходимые для routing, billing и safety boundary.

## 8.2. Required fields

Gateway должен извлечь:

```text
body.model
```

Если `model` отсутствует или пустой:

```text
HTTP 400
error.code = model_required
```

## 8.3. Stream

Первая версия не поддерживает streaming.

Если body содержит:

```json
{
  "stream": true
}
```

Ответ:

```text
HTTP 400
error.code = streaming_unsupported
```

## 8.4. Requested capabilities

Gateway должен определить requested capabilities на основе структурных полей запроса.

Примеры:

```text
tools present             -> tools
tool_choice present       -> tool_choice
response_format present   -> response_format
image content present     -> image_input
audio content present     -> audio_input
file/document present     -> file_input
video content present     -> video_input
reasoning_effort present  -> reasoning
```

Capability detection должна быть structural, а не semantic.

Запрещено определять capability по смыслу текста prompt.

## 8.5. Routing

Gateway должен найти routes по ключу:

```text
api_family = openai_compatible
endpoint_kind = chat
client_model = body.model
```

Если model неизвестна:

```text
HTTP 400
error.code = unknown_model
```

Если нет route с нужными capabilities:

```text
HTTP 400
error.code = unsupported_capability
```

Если routes есть, но все недоступны:

```text
HTTP 503
error.code = no_route_available
```

## 8.6. Upstream forwarding

Gateway отправляет upstream body без изменений.

Gateway может изменить только:

```text
target base URL
path, если provider adapter требует provider-compatible path для той же API family
Authorization header
hop-by-hop headers
```

Request semantic payload не меняется.

## 8.7. Response

Successful upstream response body возвращается без изменений.

Gateway добавляет billing headers.

---

# 9. POST /v1/embeddings

## 9.1. Request

```http
POST /v1/embeddings
Authorization: Bearer sk_...
Content-Type: application/json
```

## 9.2. Required fields

Gateway должен извлечь:

```text
body.model
```

Если `model` отсутствует или пустой:

```text
HTTP 400
error.code = model_required
```

## 9.3. Routing key

```text
api_family = openai_compatible
endpoint_kind = embeddings
client_model = body.model
```

Route должен иметь capability:

```text
embeddings = true
```

## 9.4. Response

Successful upstream response body возвращается без изменений.

Gateway добавляет billing headers.

---

# 10. POST /v1/images/generations

## 10.1. Request

```http
POST /v1/images/generations
Authorization: Bearer sk_...
Content-Type: application/json
```

## 10.2. Required fields

Gateway должен извлечь:

```text
body.model
```

Если `model` отсутствует или пустой:

```text
HTTP 400
error.code = model_required
```

## 10.3. Routing key

```text
api_family = openai_compatible
endpoint_kind = images_generation
client_model = body.model
```

Route должен иметь capability:

```text
images_generation = true
```

## 10.4. Response

Successful upstream response body возвращается без изменений.

Gateway добавляет billing headers.

---

# 11. Billing response headers

Gateway должен добавлять billing headers к successful LLM responses.

Минимальный набор:

```http
X-Local-Request-ID: llmreq_...
X-Billing-Provider-Type: openrouter
X-Billing-Client-Model: gpt-4.1-mini
X-Billing-Model: openrouter:gpt-4.1-mini
X-Billing-Input-Tokens: 100
X-Billing-Cached-Input-Tokens: 0
X-Billing-Output-Tokens: 50
X-Billing-Reasoning-Tokens: 0
X-Billing-Image-Input-Tokens: 0
X-Billing-Audio-Input-Tokens: 0
X-Billing-Audio-Output-Tokens: 0
X-Billing-File-Input-Tokens: 0
X-Billing-Video-Input-Tokens: 0
X-Billing-Amount-Cents: 12
X-Billing-Currency: RUB
X-Wallet-Balance-Cents: 10000
X-Wallet-Effective-Balance-Cents: 9988
X-Billing-Pending-Cents: 12
```

Headers не должны содержать:

```text
reseller API key
provider API key
raw user API key
billing JWT
internal route price before markup, если это не public contract
```

---

# 12. Error envelope

Gateway-owned errors должны возвращаться в едином формате:

```json
{
  "error": {
    "code": "unknown_model",
    "message": "Unknown model",
    "request_id": "llmreq_..."
  }
}
```

`request_id` должен быть заполнен, если он уже создан.

Error model подробно описывается в:

```text
docs/spec/080-error-model.ru.md
```

---

# 13. Method restrictions

Если endpoint существует, но method неверный:

```text
HTTP 405
error.code = method_not_allowed
```

Если endpoint неизвестен:

```text
HTTP 404
error.code = not_found
```

---

# 14. Body size limit

Gateway должен ограничивать размер request body.

Config key:

```text
TOKENIO_REQUEST_BODY_MAX_BYTES
```

Если body больше лимита:

```text
HTTP 413
error.code = request_body_too_large
```

---

# 15. Content-Type

Для POST endpoints первой версии ожидается:

```http
Content-Type: application/json
```

Если `Content-Type` отсутствует, gateway может попытаться обработать body как JSON.

Если body невалидный JSON:

```text
HTTP 400
error.code = invalid_json
```

---

# 16. Streaming

Streaming в первой версии не поддерживается.

Если client request явно требует streaming:

```text
HTTP 400
error.code = streaming_unsupported
```

Gateway не должен silently downgrade streaming request в non-streaming.

---

# 17. Compatibility invariant

Public `/v1/*` endpoints первой версии являются OpenAI-compatible surface.

Gateway не должен принимать request body одного API family и отправлять его route другого API family.

Запрещено:

```text
OpenAI-compatible body -> Gemini-native route
Gemini-native body -> OpenAI-compatible route
Anthropic-native body -> OpenAI-compatible route
Ollama-native body -> OpenAI-compatible route
```

Fallback разрешён только внутри того же:

```text
api_family
endpoint_kind
client_model
```
