# 0001. Tokenio Gateway как provider-agnostic LLM billing gateway

## Статус

Accepted

## Контекст

Tokenio Gateway должен быть единой точкой доступа к LLM API для приложений, агентов, SDK и фреймворков.

Клиент подключает gateway как обычный LLM provider:

```text
base_url = https://<tokenio-domain>/v1
api_key  = sk_...
```

Клиент не должен знать:

```text
reseller
provider account
provider API key
internal route
internal route price
internal billing ledger
```

Gateway должен сам выбрать подходящий upstream route, проверить баланс, учесть usage, рассчитать стоимость и вернуть клиенту ответ в том же API-формате.

## Решение

Tokenio Gateway строится как provider-agnostic LLM billing and reseller routing gateway.

Основные runtime-сущности:

```text
provider_type
reseller
route
api_family
endpoint_kind
client_model
provider_model
model_rewrite_policy
capabilities
route_price
usage_record
```

Маршрутизация выполняется по ключу:

```text
api_family + endpoint_kind + client_model
```

Fallback разрешён только между routes с одинаковыми:

```text
api_family
endpoint_kind
client_model
```

Route selection должен выбирать самый дешёвый доступный route с учётом:

```text
capabilities
cooldown
reseller balance
route limits
estimated upstream cost
```

## Инварианты

* Клиент передаёт `Authorization: Bearer sk_...`.
* Клиентский JWT не является публичным auth contract.
* User API key hashing в production contract выполняется через HMAC-SHA256 с `TOKENIO_API_KEY_HASH_SECRET`.
* SHA-256 без secret не является допустимым production fallback.

* Billing JWT используется только внутри gateway.
* Request semantic payload не конвертируется; допускается только explicit model alias rewrite.
* Response body не конвертируется.
* Billing metadata возвращается через response headers.
* Provider-specific behavior не должен попадать в generic gateway layers.
* Reseller API keys загружаются через `api_key_env`.
* Public `/billing/flush` отсутствует.
* Auto-charge выполняется внутри gateway.
* Route selection не должен выбирать route только по `model`.
* Если `provider_model` отличается от `client_model`, route обязан явно включить `model_rewrite_policy = provider_model`.
* Model rewrite не разрешает API-format conversion.


## Последствия

Generic HTTP layer отвечает только за:

```text
path dispatch
auth boundary
body size limit
request id
calling application services
writing response
```

Generic routing layer отвечает только за нормализованные route metadata:

```text
api_family
endpoint_kind
client_model
capabilities
price
limits
cooldown
balance
```

Provider-specific behavior изолируется в отдельных adapters:

```text
forwarding adapter
usage extractor
error classifier
capability mapper
token estimator
```

Новые providers добавляются через metadata и adapters, а не через условия в generic HTTP handlers.

## Запрещено

* Добавлять provider-specific hacks в HTTP handlers.
* Выбирать route только по `model`.
* Делать fallback между разными API families.
* Конвертировать semantic request payload между OpenAI/Gemini/Anthropic/Ollama formats.
* Возвращать клиенту изменённый response body.
* Возвращать публичный `/billing/flush`.
* Использовать raw user API key для billing service.
* Использовать reseller API key для user auth.
* Смешивать route selection, billing ledger и forwarding в одном handler.
