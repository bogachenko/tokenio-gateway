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

