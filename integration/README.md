# Integration tests

Integration tests live under this directory and are built only with:

```bash
go test -tags=integration ./integration/...
```

Rules:

- do not call production services;
- use local Docker Compose dependencies or in-process fakes;
- keep fake services deterministic;
- every added scenario must document the exact fake dependency it uses.
## PostgreSQL dependency

Start local Postgres with the checked-in Docker Compose stack:

```bash
./scripts/integration-postgres-up.sh
```

Run the Postgres integration test with the printed DSN:

```bash
TOKENIO_INTEGRATION_DATABASE_DSN='postgres://tokenio:tokenio_dev_password@127.0.0.1:5432/tokenio?sslmode=disable' go test -tags=integration ./integration/...
```

Stop and remove local state:

```bash
./scripts/integration-postgres-down.sh
```
## Fake Billing service

The reusable fake Billing service lives in `integration/fakes/billing`. It is an
in-process `httptest.Server` with programmable responses and request recording.
Use it from integration scenarios instead of calling external Billing services.
## Fake OpenAI-compatible upstream

The reusable fake OpenAI-compatible upstream lives in `integration/fakes/openaicompat`.
It supports deterministic defaults for `/v1/models`, `/v1/chat/completions`,
`/v1/embeddings` and `/v1/images/generations`, plus programmable responses and
request recording.
## Fake Anthropic upstream

The reusable fake Anthropic upstream lives in `integration/fakes/anthropic`.
It supports deterministic defaults for `POST /v1/messages`, plus programmable
responses and request recording.
## Fake Gemini upstream

The reusable fake Gemini upstream lives in `integration/fakes/gemini`. It supports
deterministic defaults for `GET /v1beta/models`, `generateContent`, `embedContent`
and `batchEmbedContents`, plus programmable responses and request recording.
## Fake Ollama upstream

The reusable fake Ollama upstream lives in `integration/fakes/ollama`. It supports
deterministic defaults for `GET /api/tags`, `POST /api/chat`, `POST /api/generate`
and `POST /api/embeddings`, plus programmable responses and request recording.
## Fake Telegram API

The reusable fake Telegram API lives in `integration/fakes/telegram`. It supports
`POST /bot<TOKEN>/sendMessage`, programmable responses and request recording.
Use it instead of the real Telegram Bot API in integration scenarios.
## Migrations and gateway lifecycle

Run a local lifecycle smoke using the checked-in Docker Compose stack:

```bash
./scripts/integration-lifecycle-smoke.sh
```

The script starts Postgres, applies migrations, starts the gateway smoke path and
then removes local integration state.
## External service rule

Integration tests must not call external real services. Use only:

- Docker Compose dependencies from this repository;
- in-process fakes under `integration/fakes`;
- local test-only URLs provided by scripts.

The `integration/no_external_services_test.go` audit fails if integration files reference
known external service markers.
## Fake service success scenario

`integration/fake_success_test.go` verifies that every checked-in fake dependency has
a deterministic successful default response.
## Fake service invalid request scenario

`integration/fake_invalid_request_test.go` verifies that every checked-in fake
dependency can deterministically return an invalid request response.
## Fake service authentication failure scenario

`integration/fake_authentication_failure_test.go` verifies that every checked-in fake
dependency can deterministically return an authentication failure response.
## Fake service rate limit scenario

`integration/fake_rate_limit_test.go` verifies that every checked-in fake dependency
can deterministically return a rate-limit response with `Retry-After`.
## Fake service quota exhausted scenario

`integration/fake_quota_exhausted_test.go` verifies that every checked-in fake
dependency can deterministically return a quota exhausted response.
## Fake service provider 5xx scenario

`integration/fake_provider_5xx_test.go` verifies that every checked-in fake
dependency can deterministically return a provider/server 5xx response.
## Fake service timeout scenario

`integration/fake_timeout_test.go` verifies a deterministic local timeout using an
in-process delayed `httptest.Server`.
## Fake service connection reset scenario

`integration/fake_connection_reset_test.go` verifies a deterministic local connection
reset using a TCP listener that accepts and immediately closes the connection.
## Fake service headers received, body failed scenario

`integration/fake_headers_body_failed_test.go` verifies a deterministic case where
HTTP headers are received successfully, but the response body fails before the declared
`Content-Length` is satisfied.
## Fake service missing usage scenario

`integration/fake_missing_usage_test.go` verifies deterministic successful responses
that intentionally omit provider usage fields.

