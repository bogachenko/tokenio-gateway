# Tokenio Gateway — спецификация

Версия: 0.1
Статус: draft
Язык: русский
Проект: `github.com/bogachenko/tokenio-gateway`

---

# 1. Назначение продукта

## 1.1. Что такое Tokenio Gateway

Tokenio Gateway — это единая точка доступа к LLM API для приложений, агентов, SDK и фреймворков.

Клиент подключает Tokenio Gateway как обычный LLM provider, указывая:

```text
base_url = https://<tokenio-domain>/v1
api_key  = sk_...
```

После этого клиент продолжает отправлять стандартные LLM-запросы в привычном API-формате:

```text
POST /v1/chat/completions
POST /v1/embeddings
POST /v1/images/generations
GET  /v1/models
```

Tokenio Gateway принимает запрос, а затем:

1. Валидирует пользовательский API key.
2. Определяет пользователя.
3. Определяет API family и endpoint kind.
4. Извлекает client model.
5. Находит совместимые reseller routes.
6. Выбирает самый дешёвый доступный route.
7. Проверяет capabilities.
8. Проверяет баланс пользователя.
9. Проверяет расчётный баланс reseller.
10. Резервирует локальный usage.
11. Проксирует запрос к выбранному reseller.
12. Получает ответ.
13. Извлекает usage.
14. Рассчитывает стоимость.
15. Сохраняет usage в локальный ledger.
16. Автоматически списывает pending usage через billing service при достижении threshold.
17. Возвращает клиенту оригинальный response body.
18. Добавляет billing metadata в response headers.

---

# 2. Главный продуктовый инвариант

Tokenio Gateway не является SDK-конвертером и не является нормализатором LLM API.

Tokenio Gateway — это billing forwarding layer и reseller routing gateway.

Главный инвариант:

```text
request API format must be preserved
```

Это означает:

```text
Client request body is not converted.
Upstream response body is not converted.
Billing metadata is returned through headers.
```

Если клиент отправил OpenAI-compatible request, Tokenio Gateway может отправить его только в route, который принимает OpenAI-compatible API.

Если клиент отправил Gemini-native request, Tokenio Gateway может отправить его только в route, который принимает Gemini-native API.

Если клиент отправил Anthropic-native request, Tokenio Gateway может отправить его только в route, который принимает Anthropic-native API.

Fallback между несовместимыми API families запрещён.

---

# 3. Границы ответственности

## 3.1. Tokenio Gateway отвечает за

Tokenio Gateway отвечает за:

1. Единый внешний LLM base URL.
2. Пользовательские API keys.
3. Маппинг API key → user_id.
4. Получение или построение billing JWT для billing service.
5. Хранение reseller routes.
6. Хранение reseller balances.
7. Хранение route prices.
8. Хранение capabilities.
9. Выбор route.
10. Retry между совместимыми routes.
11. Cooldown routes.
12. Проверку пользовательского баланса.
13. Локальный usage ledger.
14. Автоматическое списание pending usage.
15. Проксирование запроса к reseller.
16. Извлечение usage из ответа reseller.
17. Conservative token estimation, если usage отсутствует или неполный.
18. Расчёт цены в RUB cents.
19. Billing headers в response.
20. Admin API для управления resellers, routes, prices, balances и API keys.
21. Telegram alerts по reseller balance.

## 3.2. Tokenio Gateway не отвечает за

Tokenio Gateway не отвечает за:

1. Семантическую конвертацию OpenAI ↔ Gemini ↔ Anthropic.
2. Конвертацию tools/function calling между API formats.
3. Конвертацию multimodal payload между API formats.
4. Изменение request body клиента.
5. Изменение response body reseller.
6. Обучение моделей.
7. Хранение пользовательских prompts как бизнес-данных.
8. Интерпретацию содержимого сообщений.
9. Принятие бизнес-решений на основе текста запроса.
10. Автоматическую покупку баланса у reseller.
11. Самостоятельное изменение provider prices без admin action.

---

# 4. Термины

## 4.1. Client

Client — приложение, агент, SDK или framework, который отправляет LLM-запросы в Tokenio Gateway.

Примеры clients:

```text
Kilo Code
Roo Code
Google ADK
LangChain
OpenAI SDK
custom backend service
```

Client знает только:

```text
base_url
api_key
model
standard request body
```

Client не знает:

```text
reseller
provider account
provider API key
route price
internal billing ledger
```

---

## 4.2. User API key

User API key — внешний ключ пользователя для доступа к Tokenio Gateway.

Формат:

```text
Authorization: Bearer sk_...
```

User API key не является JWT.

User API key не передаётся в billing service.

User API key не передаётся reseller.

Tokenio Gateway валидирует user API key, находит пользователя и использует внутренний billing JWT для вызовов billing service.

---

## 4.3. Billing JWT

Billing JWT — внутренний credential для взаимодействия Tokenio Gateway с billing service от имени пользователя.

Billing JWT используется только на участке:

```text
Tokenio Gateway -> Billing Service
```

Billing JWT не должен быть виден клиенту.

---

## 4.4. Provider type

Provider type — тип upstream/provider API или reseller ecosystem.

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

Provider type не равен reseller.

Provider type описывает класс провайдера или upstream protocol family, но не конкретный аккаунт.

---

## 4.5. Reseller

Reseller — конкретный upstream account/base_url/API key/balance/limits.

Пример:

```text
reseller_id: hydra_main
provider_type: hydra
base_url: https://...
api_key_env: HYDRA_MAIN_API_KEY
balance_cents: 500000
```

Другой пример:

```text
reseller_id: openrouter_primary
provider_type: openrouter
base_url: https://...
api_key_env: OPENROUTER_PRIMARY_API_KEY
balance_cents: 250000
```

Reseller хранит ссылку на env-переменную с API key:

```text
api_key_env
```

Сам секрет хранится в environment, а не в открытом конфиге.

---

## 4.6. Route

Route — возможность купить конкретную client model у конкретного reseller через конкретный API family и endpoint kind.

Route связывает:

```text
client_model
provider_model
provider_type
reseller_id
api_family
endpoint_kind
capabilities
prices
limits
cooldown
```

Пример route:

```text
client_model: gpt-4.1-mini
provider_model: openai/gpt-4.1-mini
provider_type: openrouter
reseller_id: openrouter_primary
api_family: openai_compatible
endpoint_kind: chat
```

Другой пример route:

```text
client_model: gemini-2.5-flash
provider_model: gemini-2.5-flash
provider_type: gemini
reseller_id: google_gemini_primary
api_family: gemini_native
endpoint_kind: chat
```

---

## 4.7. Client model

Client model — имя модели, которое видит клиент.

Пример:

```text
gpt-4.1-mini
gemini-2.5-flash
claude-3-5-sonnet
```

Client model используется во внешнем API и в `/v1/models`.

Client model не обязан совпадать с provider model.

---

## 4.8. Provider model

Provider model — имя модели, которое нужно отправить конкретному reseller.

Пример:

```text
client_model: gpt-4.1-mini
provider_model: openai/gpt-4.1-mini
```

или:

```text
client_model: claude-3-5-sonnet
provider_model: anthropic/claude-3-5-sonnet
```

Tokenio Gateway не должен раскрывать provider model клиенту, если это не предусмотрено admin/debug API.

---

## 4.9. API family

API family — формат API, в котором клиент отправил запрос.

Минимально поддерживаемые значения:

```text
openai_compatible
gemini_native
anthropic_native
ollama_native
```

В первой публичной версии основной внешний surface:

```text
/v1/chat/completions
/v1/embeddings
/v1/images/generations
/v1/models
/health
```

---

## 4.10. Endpoint kind

Endpoint kind — нормализованный тип операции.

Поддерживаемые значения первой версии:

```text
chat
embeddings
images_generation
models
health
```

Endpoint kind используется для route selection, pricing, capability checks и usage accounting.

---

# 5. Внешний API

## 5.1. Base URL

Клиент использует Tokenio Gateway как LLM base URL.

Основной формат:

```text
https://<tokenio-domain>/v1
```

Пример итоговых endpoint URLs:

```text
GET  https://<tokenio-domain>/v1/models
POST https://<tokenio-domain>/v1/chat/completions
POST https://<tokenio-domain>/v1/embeddings
POST https://<tokenio-domain>/v1/images/generations
```

Health endpoint:

```text
GET https://<tokenio-domain>/health
```

---

## 5.2. Authentication

Все LLM endpoints, кроме `/health`, требуют user API key.

Формат:

```http
Authorization: Bearer sk_...
```

Если header отсутствует:

```text
HTTP 401
error.code = unauthorized
```

Если header имеет неверный формат:

```text
HTTP 401
error.code = unauthorized
```

Если API key не найден, отключён или отозван:

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

## 5.3. GET /health

Endpoint:

```http
GET /health
```

Назначение:

```text
Проверка, что процесс Tokenio Gateway запущен.
```

Ответ:

```text
200 OK
```

Body:

```text
OK
```

`/health` не требует user API key.

---

## 5.4. GET /v1/models

Endpoint:

```http
GET /v1/models
```

Назначение:

```text
Вернуть список client models, доступных пользователю.
```

Ответ должен показывать только публичные client-facing данные.

Минимальный response shape:

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
        "tools": true,
        "image_input": true,
        "embeddings": false,
        "images_generation": false
      }
    }
  ]
}
```

`/v1/models` не должен раскрывать:

```text
reseller_id
api_key_env
provider API key
internal balance
cooldown reason
private provider_model
```

Если одна client model доступна через несколько routes, `/v1/models` показывает public sell price по текущему самому дешёвому доступному route с учётом markup.

Если route временно недоступен из-за cooldown, `/v1/models` может:

```text
1. показать цену следующего доступного route;
2. скрыть модель, если доступных routes нет;
3. показать active=false, если модель зарегистрирована, но сейчас недоступна.
```

---

## 5.5. POST /v1/chat/completions

Endpoint:

```http
POST /v1/chat/completions
```

API family:

```text
openai_compatible
```

Endpoint kind:

```text
chat
```

Tokenio Gateway должен:

1. Прочитать request body.
2. Извлечь `model` из `body.model`.
3. Не изменять request body.
4. Определить requested capabilities.
5. Найти совместимые routes.
6. Выбрать самый дешёвый доступный route.
7. Проксировать body как есть.
8. Вернуть upstream response body как есть.
9. Добавить billing headers.

Если `body.model` отсутствует:

```text
HTTP 400
error.code = model_required
```

Если model неизвестна:

```text
HTTP 400
error.code = unknown_model
```

Если route не найден:

```text
HTTP 503
error.code = no_route_available
```

Если capability не поддерживается:

```text
HTTP 400
error.code = unsupported_capability
```

---

## 5.6. POST /v1/embeddings

Endpoint:

```http
POST /v1/embeddings
```

API family:

```text
openai_compatible
```

Endpoint kind:

```text
embeddings
```

Tokenio Gateway должен:

1. Извлечь `model` из `body.model`.
2. Не изменять request body.
3. Выбрать route с capability `embeddings=true`.
4. Проксировать request.
5. Извлечь usage.
6. Рассчитать стоимость.
7. Вернуть response body без изменений.
8. Добавить billing headers.

---

## 5.7. POST /v1/images/generations

Endpoint:

```http
POST /v1/images/generations
```

API family:

```text
openai_compatible
```

Endpoint kind:

```text
images_generation
```

Tokenio Gateway должен:

1. Извлечь `model` из `body.model`.
2. Не изменять request body.
3. Выбрать route с capability `images_generation=true`.
4. Проксировать request.
5. Извлечь usage или рассчитать conservative estimated usage.
6. Рассчитать стоимость.
7. Вернуть response body без изменений.
8. Добавить billing headers.

---

# 6. Auth model

## 6.1. Внешняя авторизация

Внешняя авторизация всегда выполняется через user API key:

```http
Authorization: Bearer sk_...
```

JWT от клиента больше не принимается как публичный auth contract.

## 6.2. Внутреннее преобразование API key

После получения API key Tokenio Gateway должен:

1. Захешировать raw API key.
2. Найти API key record в Postgres.
3. Проверить, что key enabled.
4. Проверить, что user enabled.
5. Получить `user_id`.
6. Получить или построить `billing_jwt`.
7. Использовать `billing_jwt` для вызовов billing service.

## 6.3. Хранение API keys

Raw API key не хранится.

В БД хранится:

```text
api_key_id
user_id
key_hash
key_prefix
name
enabled
created_at
last_used_at
revoked_at
```

`key_prefix` нужен только для отображения в admin API:

```text
sk_live_abcd...
```

## 6.4. Billing JWT

Billing JWT не является внешним contract.

Billing JWT может быть:

```text
1. подписан Tokenio Gateway через TOKENIO_BILLING_JWT_SIGNING_KEY;
2. либо храниться в encrypted form, если billing service требует внешний issued token.
```

В первой версии предпочтительный вариант:

```text
Tokenio Gateway сам подписывает billing JWT из user_id.
```

Минимальные claims:

```json
{
  "user_id": "<user_id>",
  "iss": "tokenio-gateway",
  "aud": "billing-service",
  "iat": 0,
  "exp": 0
}
```

TTL billing JWT должен быть коротким.

Рекомендуемый TTL:

```text
15 минут
```

---

# 7. Billing model string

Для billing service поле `model` формируется так:

```text
provider_type:client_model
```

Пример:

```text
openrouter:gpt-4.1-mini
hydra:gpt-4.1-mini
gemini:gemini-2.5-flash
anthropic:claude-3-5-sonnet
```

Важно:

```text
provider_type берётся из фактически выбранного route.
client_model берётся из клиентского запроса.
```

Если запрос был обслужен fallback route через другого reseller/provider_type, billing model string должен отражать фактически использованный route.

---

# 8. Первый implementation scope

Первая спецификация должна покрывать все целевые сущности, даже если реализация будет разбита на этапы.

Первый обязательный endpoint surface:

```text
GET  /health
GET  /v1/models
POST /v1/chat/completions
POST /v1/embeddings
POST /v1/images/generations
```

Обязательные provider types в data model:

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

Обязательные runtime-механизмы:

```text
user API keys
billing JWT conversion
reseller api_key_env
route selection
capability validation
local ledger
auto-charge threshold
idempotency-key support
reseller balance accounting
Telegram reseller balance alerts
admin API
```
