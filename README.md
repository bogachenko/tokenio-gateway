# tokenio-gateway

LLM billing and reseller routing gateway.

## Role

Tokenio Gateway is a single LLM base URL for clients and agents.

Clients send requests to Tokenio in the API format they already use.
Tokenio does not convert request bodies and does not convert response bodies.
Tokenio authenticates users with API keys, selects the cheapest available compatible reseller route, forwards the request, accounts tokens and cost, stores usage locally, and charges the external billing service automatically.

## External client auth

```http
Authorization: Bearer sk_...
```

The API key is validated by Tokenio and mapped to:

```text
user_id
billing_jwt
```

The billing JWT is internal and is used only for Tokenio -> billing service calls.

## First endpoint surface

```text
GET  /health
GET  /v1/models
POST /v1/chat/completions
POST /v1/embeddings
POST /v1/images/generations
```

## Routing invariant

```text
api_family + endpoint_kind + client_model -> compatible reseller routes
```

Fallback is allowed only between routes with the same API family and endpoint kind.

## Reseller route model

```text
provider_type = openai | openrouter | together | groq | ollama | lmstudio | vllm | gemini | anthropic | hydra
reseller      = concrete account/base_url/api_key_env/balance/limits
route         = reseller sells concrete model through concrete API family
```

## Pricing invariant

Client amount is calculated from the selected route price multiplied by route markup coefficient.

If upstream returns only total input tokens and request contains image/audio/file/video input, Tokenio bills total input tokens using the most expensive applicable input category.

## Billing invariant

Public `/billing/flush` is removed.
Tokenio keeps local pending usage and automatically charges billing when pending amount reaches `TOKENIO_AUTO_CHARGE_THRESHOLD_CENTS`.

## Configuration

```bash
export TOKENIO_GATEWAY_ADDR=':8880'
export TOKENIO_DATABASE_DSN='host=localhost user=postgres password=postgres dbname=tokenio_gateway port=5432 sslmode=disable TimeZone=UTC'
export TOKENIO_BILLING_BASE_URL='https://billing.example.com'
export TOKENIO_BILLING_SERVICE_TOKEN='internal-service-token'
export TOKENIO_BILLING_JWT_SIGNING_KEY='billing-jwt-signing-key'
export TOKENIO_COST_CURRENCY='RUB'
export TOKENIO_AUTO_CHARGE_THRESHOLD_CENTS='1000'
export TOKENIO_MIN_CHARGE_AMOUNT_CENTS='100'
export TOKENIO_MIN_REQUEST_BALANCE_CENTS='500'
export TOKENIO_TOKEN_ESTIMATION_SAFETY_FACTOR='1.25'
export TOKENIO_COST_ESTIMATION_SAFETY_FACTOR='1.10'
export TOKENIO_REQUEST_BODY_MAX_BYTES='67108864'
export TOKENIO_RESELLER_BALANCE_ALERT_CENTS='10000'
export TOKENIO_ROUTE_COOLDOWN_RATE_LIMIT='60s'
export TOKENIO_ROUTE_COOLDOWN_QUOTA_EXCEEDED='24h'
export TOKENIO_ROUTE_COOLDOWN_5XX='30s'
export TOKENIO_ROUTE_COOLDOWN_TIMEOUT='30s'
export TOKENIO_ROUTE_COOLDOWN_AUTH_ERROR='24h'
export TOKENIO_BILLING_TIMEOUT='30s'
export TOKENIO_UPSTREAM_TIMEOUT='90s'
```
