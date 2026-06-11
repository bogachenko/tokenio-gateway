#!/usr/bin/env python3
"""
Patch tokenio-gateway specification invariants.

Scope:
- SPEC ONLY. This script does not patch Go runtime code.
- Fixes the first specification-level contradictions:
  1. provider_model vs raw request body by introducing explicit model_rewrite_policy.
  2. native API families external path contract.
  3. /v1/models capabilities policy: intersection instead of unsafe union.
  4. image generation unit pricing in specs and schema.

Usage:
  cd /path/to/tokenio-gateway
  python3 patch_tokenio_gateway_spec_invariants.py

The script is idempotent:
- verifies repository shape;
- creates/updates docs/spec files;
- prints git diff;
- prints verification grep commands/results.
"""

from __future__ import annotations

import os
import subprocess
import sys
from pathlib import Path


REPO_MARKERS = [
    "go.mod",
    "docs/spec/000-tokenio-gateway.ru.md",
    "docs/spec/010-external-api.ru.md",
    "docs/spec/030-routing-and-resellers.ru.md",
    "docs/spec/040-pricing-and-usage.ru.md",
    "docs/spec/060-admin-api.ru.md",
    "docs/spec/070-database-schema.ru.md",
]


def fail(message: str) -> None:
    print(f"ERROR: {message}", file=sys.stderr)
    sys.exit(1)


def repo_root() -> Path:
    root = Path.cwd()
    missing = [p for p in REPO_MARKERS if not (root / p).exists()]
    if missing:
        fail(
            "this script must be run from tokenio-gateway repo root; missing:\n"
            + "\n".join(f"  - {p}" for p in missing)
        )

    gomod = (root / "go.mod").read_text(encoding="utf-8")
    if "github.com/bogachenko/tokenio-gateway" not in gomod:
        fail("go.mod does not look like github.com/bogachenko/tokenio-gateway")

    return root


def read(path: Path) -> str:
    return path.read_text(encoding="utf-8")


def write(path: Path, content: str) -> None:
    path.write_text(content, encoding="utf-8")


def replace_once(path: Path, old: str, new: str) -> bool:
    content = read(path)
    if old not in content:
        if new in content:
            return False
        fail(f"expected text not found in {path}:\n{old[:500]}")
    content = content.replace(old, new, 1)
    write(path, content)
    return True


def replace_all(path: Path, old: str, new: str) -> bool:
    content = read(path)
    if old not in content:
        if new in content:
            return False
        return False
    content = content.replace(old, new)
    write(path, content)
    return True


def ensure_after(path: Path, anchor: str, block: str) -> bool:
    content = read(path)
    if block.strip() in content:
        return False
    if anchor not in content:
        fail(f"anchor not found in {path}:\n{anchor[:500]}")
    content = content.replace(anchor, anchor + "\n" + block, 1)
    write(path, content)
    return True


def ensure_before(path: Path, anchor: str, block: str) -> bool:
    content = read(path)
    if block.strip() in content:
        return False
    if anchor not in content:
        fail(f"anchor not found in {path}:\n{anchor[:500]}")
    content = content.replace(anchor, block + "\n" + anchor, 1)
    write(path, content)
    return True


def ensure_file(path: Path, content: str) -> bool:
    if path.exists() and path.read_text(encoding="utf-8") == content:
        return False
    path.write_text(content, encoding="utf-8")
    return True


def run(cmd: list[str]) -> tuple[int, str]:
    p = subprocess.run(
        cmd,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        text=True,
        check=False,
    )
    return p.returncode, p.stdout


NATIVE_API_SPEC = """# 011. Native API Families

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
"""


def patch_000(root: Path) -> bool:
    changed = False
    path = root / "docs/spec/000-tokenio-gateway.ru.md"

    changed |= replace_all(
        path,
        "Client request body is not converted.\nUpstream response body is not converted.",
        "Client request semantic payload is not converted.\nOnly explicitly configured model alias rewrite is allowed.\nUpstream response body is not converted.",
    )

    changed |= ensure_after(
        path,
        """Tokenio Gateway валидирует user API key, находит пользователя и использует внутренний billing JWT для вызовов billing service.""",
        """

---

## 4.10. Model rewrite policy

Model rewrite policy — явное правило route, которое определяет, может ли provider adapter заменить только структурное поле модели перед upstream request.

Разрешённые значения:

```text
none
provider_model
```

`none` означает:

```text
request model field/path не изменяется
```

`provider_model` означает:

```text
adapter может заменить только model identifier на route.provider_model
```

Это не является API-format conversion.

Запрещено вместе с model rewrite:

```text
конвертировать messages/content/tools
конвертировать multimodal payload
конвертировать response_format
менять semantic request payload
делать fallback в другую API family
```

Для OpenAI-compatible request разрешённая mutation ограничена:

```text
body.model = route.provider_model
```

Для path-based native APIs разрешённая mutation ограничена model segment в path, если соответствующий adapter явно поддерживает это.

""",
    )
    return changed


def patch_010(root: Path) -> bool:
    changed = False
    path = root / "docs/spec/010-external-api.ru.md"

    changed |= ensure_after(
        path,
        """Публичные `/v1/*` endpoints первой версии относятся к:

```text
api_family = openai_compatible
```""",
        """

Native API family paths описываются отдельно:

```text
docs/spec/011-native-api-families.ru.md
```

Эти paths нужны для drop-in совместимости с SDK, которые не используют OpenAI-compatible `/v1/chat/completions`.

""",
    )

    changed |= replace_all(
        path,
        "Tokenio Gateway не должен изменять request body клиента.",
        "Tokenio Gateway не должен изменять semantic request payload клиента.",
    )

    changed |= ensure_after(
        path,
        """Прочитанный body должен быть отправлен upstream route без изменений.""",
        """

Единственное допустимое исключение — явная model alias rewrite policy.

Если route имеет:

```text
model_rewrite_policy = provider_model
```

provider adapter может заменить только model identifier:

```text
OpenAI-compatible: body.model
Native path-based APIs: model path segment, если adapter это явно поддерживает
```

Любые другие изменения body запрещены.

Если route требует `provider_model`, но adapter не поддерживает безопасную model rewrite для данного API family, route считается invalid.

""",
    )

    changed |= ensure_after(
        path,
        """Request semantic payload не меняется.""",
        """

Если route явно настроен с:

```text
model_rewrite_policy = provider_model
```

adapter может заменить только model identifier на `route.provider_model`.

Это не разрешает конвертировать API format или менять semantic payload.

""",
    )

    return changed


def patch_030(root: Path) -> bool:
    changed = False
    path = root / "docs/spec/030-routing-and-resellers.ru.md"

    changed |= replace_once(
        path,
        """Route содержит:

```text
id
reseller_id
provider_type
api_family
endpoint_kind
client_model
provider_model
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
```""",
        """Route содержит:

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
```""",
    )

    old_provider_model = """## 5.4. Provider model

`provider_model` — имя модели, которое нужно отправить upstream reseller.

Пример:

```text
client_model: gpt-4.1-mini
provider_model: openai/gpt-4.1-mini
```

Если API family и reseller позволяют использовать client model без изменения, тогда:

```text
provider_model = client_model
```

В первой версии request body не конвертируется. Поэтому для OpenAI-compatible `/v1/*` routes `provider_model` может использоваться только если provider adapter умеет безопасно передать модель без изменения semantic payload.

Если замена `body.model` потребуется для конкретного upstream, это считается provider adapter behavior и должно быть явно описано отдельно.
"""
    new_provider_model = """## 5.4. Provider model

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
"""
    changed |= replace_once(path, old_provider_model, new_provider_model)

    changed |= ensure_after(
        path,
        new_provider_model,
        """

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

""",
    )

    changed |= replace_all(
        path,
        "capabilities = union of enabled routes for this client_model and endpoint type",
        "capabilities = intersection of available routes for this client_model and endpoint type",
    )

    changed |= ensure_after(
        path,
        """Решение первой версии для capabilities:

```text
capabilities = intersection of available routes for this client_model and endpoint type
```""",
        """

Причина:

```text
/v1/models не должен обещать combination capabilities,
если ни один concrete route не может обслужить такой request.
```

Будущая версия может вернуть `capability_profiles[]`, но первая версия использует conservative intersection.

""",
    )

    return changed


def patch_040(root: Path) -> bool:
    changed = False
    path = root / "docs/spec/040-pricing-and-usage.ru.md"

    changed |= replace_once(
        path,
        """Tokenio Gateway нормализует usage в структуру:

```text
input_tokens
cached_input_tokens
output_tokens
reasoning_tokens
image_input_tokens
audio_input_tokens
audio_output_tokens
file_input_tokens
video_input_tokens
```""",
        """Tokenio Gateway нормализует usage в структуру:

```text
input_tokens
cached_input_tokens
output_tokens
reasoning_tokens
image_input_tokens
audio_input_tokens
audio_output_tokens
file_input_tokens
video_input_tokens
image_generation_units
```""",
    )

    changed |= ensure_after(
        path,
        """## 4.10. video_input_tokens

`video_input_tokens` — input tokens или token-equivalent units, связанные с video input.

Если provider возвращает video token breakdown, значение берётся из provider usage.

Если provider не возвращает breakdown, применяется multimodal max input rule.
""",
        """

## 4.11. image_generation_units

`image_generation_units` — нормализованные billable units для `/v1/images/generations`.

Первая версия использует deterministic unit rule:

```text
image_generation_units = request.n OR 1
```

Provider-specific adapter может вернуть более точное значение, если upstream тарифицирует generation иначе.

Если route не имеет цены для image generation units и adapter не может вернуть token-equivalent usage, route считается invalid для `images_generation`.

""",
    )

    changed |= replace_once(
        path,
        """video_input_price_per_1m_tokens_cents
markup_coefficient
enabled
created_at
updated_at
```""",
        """video_input_price_per_1m_tokens_cents
image_generation_price_per_unit_cents
image_generation_unit_kind
markup_coefficient
enabled
created_at
updated_at
```""",
    )

    changed |= replace_once(
        path,
        """+ video_input_tokens   * video_input_price_per_1m_tokens_cents
```""",
        """+ video_input_tokens   * video_input_price_per_1m_tokens_cents
+ image_generation_units * image_generation_price_per_unit_cents * 1_000_000
```""",
    )

    changed |= ensure_after(
        path,
        """Так как prices заданы за 1M tokens, сумма в cents:

```text
amount_before_markup_cents =
  total_raw / 1_000_000
```""",
        """

`image_generation_price_per_unit_cents` уже задан в cents per unit.

Чтобы сохранить single rounding formula, unit price участвует в `total_raw` как:

```text
image_generation_units * image_generation_price_per_unit_cents * 1_000_000
```

""",
    )

    old_images = """## 11.3. Images generations

Для `/v1/images/generations` provider может не возвращать token usage.

Usage может быть выражен через:

```text
image generation units
estimated prompt tokens
provider-specific cost units
```

В первой версии images generation pricing должен быть route-specific и conservative.

Допустимые варианты route price для image generation:

```text
1. token-equivalent pricing through image_input_tokens/output units;
2. fixed price per generated image represented as provider-specific normalized units;
3. provider-specific adapter returns normalized token-equivalent usage.
```

Generic pricing layer не должен знать конкретную image provider schema.

Если image generation usage нельзя оценить до запроса, route должен быть disabled для images_generation до настройки adapter/pricing.
"""
    new_images = """## 11.3. Images generations

Для `/v1/images/generations` provider может не возвращать token usage.

Первая версия поддерживает два production-safe pricing режима:

```text
1. fixed unit pricing через image_generation_units;
2. provider adapter returns normalized token-equivalent usage.
```

Fixed unit pricing:

```text
image_generation_units = request.n OR 1
client_amount uses route.image_generation_price_per_unit_cents
```

Route должен иметь:

```text
image_generation_price_per_unit_cents > 0
image_generation_unit_kind = generated_image
```

Если provider-specific adapter возвращает token-equivalent usage, pricing может использовать обычные token categories.

Generic pricing layer не должен знать конкретную image provider schema.

Если image generation usage нельзя оценить до upstream request и route не имеет fixed unit price:

```text
HTTP 503
error.code = pricing_unavailable
```

Request не должен быть отправлен upstream.

"""
    changed |= replace_once(path, old_images, new_images)

    changed |= replace_once(
        path,
        """video input pricing
multimodal max input rule""",
        """video input pricing
image generation unit pricing
multimodal max input rule""",
    )

    changed |= replace_once(
        path,
        """1. Usage нормализуется в фиксированные categories.""",
        """1. Usage нормализуется в фиксированные categories, включая image_generation_units.""",
    )

    return changed


def patch_060(root: Path) -> bool:
    changed = False
    path = root / "docs/spec/060-admin-api.ru.md"

    changed |= replace_once(
        path,
        """"provider_model": "openai/gpt-4.1-mini",
  "enabled": true,""",
        """"provider_model": "openai/gpt-4.1-mini",
  "model_rewrite_policy": "provider_model",
  "enabled": true,""",
    )

    changed |= replace_once(
        path,
        """"provider_model": "openai/gpt-4.1-mini",
  "enabled": true,
  "priority": 100,""",
        """"provider_model": "openai/gpt-4.1-mini",
  "model_rewrite_policy": "provider_model",
  "enabled": true,
  "priority": 100,""",
    )

    changed |= replace_once(
        path,
        """provider_model must be non-empty
default_max_output_tokens required for chat routes""",
        """provider_model must be non-empty
model_rewrite_policy must be one of: none, provider_model
if provider_model != client_model, model_rewrite_policy must be provider_model
default_max_output_tokens required for chat routes""",
    )

    changed |= replace_once(
        path,
        """provider_model
enabled
priority""",
        """provider_model
model_rewrite_policy
enabled
priority""",
    )

    changed |= replace_all(
        path,
        """"video_input_price_per_1m_tokens_cents": 10000,
    "markup_coefficient": 1.3,""",
        """"video_input_price_per_1m_tokens_cents": 10000,
    "image_generation_price_per_unit_cents": 150,
    "image_generation_unit_kind": "generated_image",
    "markup_coefficient": 1.3,""",
    )

    changed |= replace_once(
        path,
        """markup_coefficient > 0
route_id must exist""",
        """markup_coefficient > 0
image_generation_price_per_unit_cents >= 0
image_generation_unit_kind must be one of: none, generated_image
route_id must exist""",
    )

    changed |= replace_once(
        path,
        """api_family allowed values
endpoint_kind allowed values
currency = RUB""",
        """api_family allowed values
endpoint_kind allowed values
model_rewrite_policy allowed values
currency = RUB""",
    )

    return changed


def patch_070(root: Path) -> bool:
    changed = False
    path = root / "docs/spec/070-database-schema.ru.md"

    changed |= replace_once(
        path,
        """    client_model TEXT NOT NULL,
    provider_model TEXT NOT NULL,

    enabled BOOLEAN NOT NULL DEFAULT TRUE,""",
        """    client_model TEXT NOT NULL,
    provider_model TEXT NOT NULL,
    model_rewrite_policy TEXT NOT NULL DEFAULT 'none',

    enabled BOOLEAN NOT NULL DEFAULT TRUE,""",
    )

    changed |= ensure_after(
        path,
        """ALTER TABLE tokenio_routes
ADD CONSTRAINT tokenio_routes_endpoint_kind_chk
CHECK (
    endpoint_kind IN (
        'chat',
        'embeddings',
        'images_generation'
    )
);
```""",
        """

## 8.5. Model rewrite policy constraint

Allowed values:

```text
none
provider_model
```

```sql
ALTER TABLE tokenio_routes
ADD CONSTRAINT tokenio_routes_model_rewrite_policy_chk
CHECK (
    model_rewrite_policy IN (
        'none',
        'provider_model'
    )
);
```

Route validation in application layer must enforce:

```text
if provider_model != client_model, model_rewrite_policy = provider_model
```

""",
    )

    # Renumber old headings only if original headings still exist.
    changed |= replace_all(path, "## 8.5. Unique route identity", "## 8.6. Unique route identity")
    changed |= replace_all(path, "## 8.6. Lookup index", "## 8.7. Lookup index")
    changed |= replace_all(path, "## 8.7. Cooldown index", "## 8.8. Cooldown index")
    changed |= replace_all(path, "## 8.8. Capabilities JSON", "## 8.9. Capabilities JSON")

    changed |= replace_once(
        path,
        """    video_input_price_per_1m_tokens_cents BIGINT NOT NULL DEFAULT 0,

    markup_coefficient NUMERIC(12, 6) NOT NULL DEFAULT 1.0,""",
        """    video_input_price_per_1m_tokens_cents BIGINT NOT NULL DEFAULT 0,

    image_generation_price_per_unit_cents BIGINT NOT NULL DEFAULT 0,
    image_generation_unit_kind TEXT NOT NULL DEFAULT 'none',

    markup_coefficient NUMERIC(12, 6) NOT NULL DEFAULT 1.0,""",
    )

    changed |= replace_once(
        path,
        """    CHECK (video_input_price_per_1m_tokens_cents >= 0),
    CHECK (markup_coefficient > 0)
);""",
        """    CHECK (video_input_price_per_1m_tokens_cents >= 0),
    CHECK (image_generation_price_per_unit_cents >= 0),
    CHECK (image_generation_unit_kind IN ('none', 'generated_image')),
    CHECK (markup_coefficient > 0)
);""",
    )

    changed |= replace_once(
        path,
        """    estimated_input_tokens BIGINT NOT NULL DEFAULT 0,
    estimated_output_tokens BIGINT NOT NULL DEFAULT 0,
    estimated_client_amount_cents BIGINT NOT NULL DEFAULT 0,""",
        """    estimated_input_tokens BIGINT NOT NULL DEFAULT 0,
    estimated_output_tokens BIGINT NOT NULL DEFAULT 0,
    estimated_image_generation_units BIGINT NOT NULL DEFAULT 0,
    estimated_client_amount_cents BIGINT NOT NULL DEFAULT 0,""",
    )

    changed |= replace_once(
        path,
        """    video_input_tokens BIGINT NOT NULL DEFAULT 0,

    client_amount_cents BIGINT NOT NULL DEFAULT 0,""",
        """    video_input_tokens BIGINT NOT NULL DEFAULT 0,
    image_generation_units BIGINT NOT NULL DEFAULT 0,

    client_amount_cents BIGINT NOT NULL DEFAULT 0,""",
    )

    changed |= replace_once(
        path,
        """    CHECK (estimated_input_tokens >= 0),
    CHECK (estimated_output_tokens >= 0),
    CHECK (estimated_client_amount_cents >= 0),""",
        """    CHECK (estimated_input_tokens >= 0),
    CHECK (estimated_output_tokens >= 0),
    CHECK (estimated_image_generation_units >= 0),
    CHECK (estimated_client_amount_cents >= 0),""",
    )

    changed |= replace_once(
        path,
        """    CHECK (video_input_tokens >= 0),

    CHECK (client_amount_cents >= 0),""",
        """    CHECK (video_input_tokens >= 0),
    CHECK (image_generation_units >= 0),

    CHECK (client_amount_cents >= 0),""",
    )

    changed |= replace_once(
        path,
        """8. Route prices support all token categories.""",
        """8. Route prices support all token categories and image generation unit pricing.""",
    )

    return changed


def patch_adr(root: Path) -> bool:
    changed = False
    path = root / "docs/adr/0001-tokenio-gateway-architecture.ru.md"

    changed |= replace_all(
        path,
        "* Request body не конвертируется.",
        "* Request semantic payload не конвертируется; допускается только explicit model alias rewrite.",
    )

    changed |= replace_all(
        path,
        "Request body не конвертируется.",
        "Request semantic payload не конвертируется; допускается только explicit model alias rewrite.",
    )

    changed |= ensure_after(
        path,
        """* Route selection не должен выбирать route только по `model`.""",
        """
* Если `provider_model` отличается от `client_model`, route обязан явно включить `model_rewrite_policy = provider_model`.
* Model rewrite не разрешает API-format conversion.
""",
    )

    changed |= replace_all(
        path,
        "* Конвертировать request body между OpenAI/Gemini/Anthropic/Ollama formats.",
        "* Конвертировать semantic request payload между OpenAI/Gemini/Anthropic/Ollama formats.",
    )

    return changed


def main() -> int:
    root = repo_root()
    changed_files: list[str] = []

    patches = [
        ("docs/spec/000-tokenio-gateway.ru.md", patch_000),
        ("docs/spec/010-external-api.ru.md", patch_010),
        ("docs/spec/030-routing-and-resellers.ru.md", patch_030),
        ("docs/spec/040-pricing-and-usage.ru.md", patch_040),
        ("docs/spec/060-admin-api.ru.md", patch_060),
        ("docs/spec/070-database-schema.ru.md", patch_070),
        ("docs/adr/0001-tokenio-gateway-architecture.ru.md", patch_adr),
    ]

    native_path = root / "docs/spec/011-native-api-families.ru.md"
    if ensure_file(native_path, NATIVE_API_SPEC):
        changed_files.append(str(native_path.relative_to(root)))

    for rel, fn in patches:
        if fn(root):
            changed_files.append(rel)

    print("Changed files:")
    if changed_files:
        for f in changed_files:
            print(f"  - {f}")
    else:
        print("  no changes; patch already applied")

    print("\n--- git diff ---")
    code, out = run(["git", "diff", "--"] + changed_files if changed_files else ["git", "diff"])
    print(out)

    print("\n--- verification grep ---")
    grep_patterns = [
        "model_rewrite_policy",
        "011-native-api-families",
        "intersection of available routes",
        "image_generation_price_per_unit_cents",
        "image_generation_units",
        "semantic request payload",
    ]
    for pattern in grep_patterns:
        code, out = run(["grep", "-R", "-n", pattern, "docs/spec", "docs/adr"])
        print(f"$ grep -R -n {pattern!r} docs/spec docs/adr")
        print(out if out else f"NO MATCH: {pattern}")

    print("\nDone.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
