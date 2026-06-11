# 011. Native API Families

Версия: 0.1
Статус: draft
Язык: русский
Проект: `github.com/bogachenko/tokenio-gateway`

---

# 1. Назначение документа

Этот документ фиксирует внешний path contract для native API families.

Главная цель:

```text
клиент меняет только base_url и api_key,
а SDK/agent продолжает генерировать стандартные paths своей API family.
```

Документ не описывает:

```text
pricing
ledger
database schema
admin API
provider-specific response parsing
```

---

# 2. Главный invariant

Tokenio Gateway не добавляет vendor namespace в public path, если это ломает стандартный SDK.

Правильно:

```text
OpenAI-compatible SDK -> /v1/chat/completions
Anthropic SDK         -> /v1/messages
Gemini SDK            -> /v1beta/models/{model}:generateContent
Ollama client         -> /api/chat
```

Неправильно для drop-in base_url:

```text
/anthropic/v1/messages
/gemini/v1beta/models/{model}:generateContent
/ollama/api/chat
```

Такие namespace paths могут существовать только как explicit compatibility aliases, но не как основной SDK contract.

---

# 3. OpenAI-compatible family

## 3.1. API family

```text
api_family = openai_compatible
```

## 3.2. Supported inbound paths

```text
GET  /v1/models
POST /v1/chat/completions
POST /v1/embeddings
POST /v1/images/generations
```

## 3.3. Model extraction

```text
/v1/chat/completions    -> body.model
/v1/embeddings          -> body.model
/v1/images/generations  -> body.model
```

If model is absent or empty:

```text
HTTP 400
error.code = model_required
```

---

# 4. Anthropic-native family

## 4.1. API family

```text
api_family = anthropic_native
```

## 4.2. Supported inbound paths

```text
POST /v1/messages
```

## 4.3. Endpoint kind

```text
/v1/messages -> endpoint_kind = chat
```

## 4.4. Model extraction

```text
body.model
```

## 4.5. Forwarding invariant

Anthropic-native request body can only be forwarded to routes with:

```text
api_family = anthropic_native
endpoint_kind = chat
client_model = body.model
```

Fallback to OpenAI-compatible routes is forbidden.

---

# 5. Gemini-native family

## 5.1. API family

```text
api_family = gemini_native
```

## 5.2. Supported inbound paths

```text
POST /v1beta/models/{model}:generateContent
POST /v1beta/models/{model}:streamGenerateContent
POST /v1beta/models/{model}:embedContent
POST /v1beta/models/{model}:batchEmbedContents
GET  /v1beta/models
```

Streaming remains unsupported in the first runtime implementation unless the streaming specification is added.

If a streaming path is received before streaming support exists:

```text
HTTP 400
error.code = streaming_unsupported
```

## 5.3. Endpoint kind

```text
/v1beta/models/{model}:generateContent       -> chat
/v1beta/models/{model}:streamGenerateContent -> chat
/v1beta/models/{model}:embedContent          -> embeddings
/v1beta/models/{model}:batchEmbedContents    -> embeddings
/v1beta/models                               -> models
```

## 5.4. Model extraction

For model operation paths:

```text
model is extracted from URL path segment {model}
```

Examples:

```text
/v1beta/models/gemini-2.5-flash:generateContent -> gemini-2.5-flash
/v1beta/models/gemini-embedding-001:embedContent -> gemini-embedding-001
```

## 5.5. Forwarding invariant

Gemini-native request body can only be forwarded to routes with the same:

```text
api_family
endpoint_kind
client_model
```

Fallback to OpenAI-compatible routes is forbidden.

---

# 6. Ollama-native family

## 6.1. API family

```text
api_family = ollama_native
```

## 6.2. Supported inbound paths

```text
POST /api/chat
POST /api/generate
POST /api/embeddings
GET  /api/tags
```

## 6.3. Endpoint kind

```text
/api/chat       -> chat
/api/generate   -> chat
/api/embeddings -> embeddings
/api/tags       -> models
```

## 6.4. Model extraction

```text
body.model
```

## 6.5. Forwarding invariant

Ollama-native request body can only be forwarded to routes with:

```text
api_family = ollama_native
```

Fallback to OpenAI-compatible, Anthropic-native or Gemini-native routes is forbidden.

---

# 7. Path collision policy

If two API families use the same path in the future, the conflict must be resolved by explicit ADR before implementation.

The first version has no conflict between:

```text
/v1/chat/completions
/v1/messages
/v1beta/models/{model}:generateContent
/api/chat
```

---

# 8. Request body policy

Native API families follow the same semantic invariant:

```text
request semantic payload is not converted between API families
response body is not converted
billing metadata is returned through Tokenio headers
```

Only explicit model alias rewrite is allowed according to route `model_rewrite_policy`.

---

# 9. Acceptance criteria

Native API family support is correct if:

```text
1. api_family is determined by path, not by model name.
2. OpenAI-compatible paths map to openai_compatible.
3. Anthropic /v1/messages maps to anthropic_native.
4. Gemini /v1beta/models/{model}:operation maps to gemini_native.
5. Ollama /api/* paths map to ollama_native.
6. model extraction is deterministic per path.
7. fallback never crosses API family.
8. namespace paths are not required for standard SDK compatibility.
9. tests cover path detection and model extraction for each family.
```
