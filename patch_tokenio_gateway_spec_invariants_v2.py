#!/usr/bin/env python3
"""
Robust v2 patch for tokenio-gateway specification invariants.

Why v2 exists:
- v1 used one overly strict exact text match in docs/spec/040-pricing-and-usage.ru.md.
- This version uses stable section/heading anchors and is safe after a partially applied v1.

Scope:
- SPEC/ADR ONLY.
- No Go runtime code changes.

Fixes:
1. Explicit model_rewrite_policy to resolve provider_model vs request-passthrough.
2. Native API family public paths.
3. /v1/models capabilities policy: conservative intersection.
4. Image generation unit pricing for /v1/images/generations.

Usage:
  cd /path/to/tokenio-gateway
  python3 patch_tokenio_gateway_spec_invariants_v2.py
"""

from __future__ import annotations

import re
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
    "docs/adr/0001-tokenio-gateway-architecture.ru.md",
]


def fail(message: str) -> None:
    print(f"ERROR: {message}", file=sys.stderr)
    sys.exit(1)


def run(cmd: list[str]) -> tuple[int, str]:
    p = subprocess.run(
        cmd,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        text=True,
        check=False,
    )
    return p.returncode, p.stdout


def repo_root() -> Path:
    root = Path.cwd()
    missing = [p for p in REPO_MARKERS if not (root / p).exists()]
    if missing:
        fail(
            "run this script from tokenio-gateway repo root; missing:\n"
            + "\n".join(f"  - {p}" for p in missing)
        )

    gomod = (root / "go.mod").read_text(encoding="utf-8")
    if "github.com/bogachenko/tokenio-gateway" not in gomod:
        fail("go.mod is not github.com/bogachenko/tokenio-gateway")

    return root


def read(path: Path) -> str:
    return path.read_text(encoding="utf-8")


def write_if_changed(path: Path, content: str) -> bool:
    old = read(path)
    if old == content:
        return False
    path.write_text(content, encoding="utf-8")
    return True


def ensure_contains(path: Path, block: str, *, before: str | None = None, after: str | None = None) -> bool:
    content = read(path)
    if block.strip() in content:
        return False

    if before is not None:
        idx = content.find(before)
        if idx < 0:
            fail(f"anchor not found in {path}: {before[:200]}")
        content = content[:idx] + block + "\n" + content[idx:]
        return write_if_changed(path, content)

    if after is not None:
        idx = content.find(after)
        if idx < 0:
            fail(f"anchor not found in {path}: {after[:200]}")
        idx += len(after)
        content = content[:idx] + "\n" + block + content[idx:]
        return write_if_changed(path, content)

    content = content.rstrip() + "\n\n" + block + "\n"
    return write_if_changed(path, content)


def replace_if_present(path: Path, old: str, new: str) -> bool:
    content = read(path)
    if old not in content:
        return False
    return write_if_changed(path, content.replace(old, new))


def regex_replace_once(path: Path, pattern: str, replacement: str, *, flags: int = 0) -> bool:
    content = read(path)
    new, n = re.subn(pattern, replacement, content, count=1, flags=flags)
    if n == 0:
        return False
    return write_if_changed(path, new)


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


def patch_adr(root: Path) -> bool:
    path = root / "docs/adr/0001-tokenio-gateway-architecture.ru.md"
    changed = False

    changed |= replace_if_present(
        path,
        "* Request body не конвертируется.",
        "* Request semantic payload не конвертируется; допускается только explicit model alias rewrite.",
    )
    changed |= replace_if_present(
        path,
        "Request body не конвертируется.",
        "Request semantic payload не конвертируется; допускается только explicit model alias rewrite.",
    )
    changed |= replace_if_present(
        path,
        "* Конвертировать request body между OpenAI/Gemini/Anthropic/Ollama formats.",
        "* Конвертировать semantic request payload между OpenAI/Gemini/Anthropic/Ollama formats.",
    )

    block = """* Если `provider_model` отличается от `client_model`, route обязан явно включить `model_rewrite_policy = provider_model`.
* Model rewrite не разрешает API-format conversion.
"""
    changed |= ensure_contains(
        path,
        block,
        after="* Route selection не должен выбирать route только по `model`.",
    )

    return changed


def patch_000(root: Path) -> bool:
    path = root / "docs/spec/000-tokenio-gateway.ru.md"
    changed = False

    changed |= replace_if_present(
        path,
        "Client request body is not converted.\nUpstream response body is not converted.",
        "Client request semantic payload is not converted.\nOnly explicitly configured model alias rewrite is allowed.\nUpstream response body is not converted.",
    )

    block = """
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
"""
    changed |= ensure_contains(path, block, before="\n---\n\n# 5. Внешний API")

    return changed


def patch_010(root: Path) -> bool:
    path = root / "docs/spec/010-external-api.ru.md"
    changed = False

    block = """
Native API family paths описываются отдельно:

```text
docs/spec/011-native-api-families.ru.md
```

Эти paths нужны для drop-in совместимости с SDK, которые не используют OpenAI-compatible `/v1/chat/completions`.
"""
    changed |= ensure_contains(
        path,
        block,
        after="api_family = openai_compatible\n```",
    )

    changed |= replace_if_present(
        path,
        "Tokenio Gateway не должен изменять request body клиента.",
        "Tokenio Gateway не должен изменять semantic request payload клиента.",
    )

    block2 = """
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
"""
    changed |= ensure_contains(
        path,
        block2,
        after="Прочитанный body должен быть отправлен upstream route без изменений.",
    )

    block3 = """
Если route явно настроен с:

```text
model_rewrite_policy = provider_model
```

adapter может заменить только model identifier на `route.provider_model`.

Это не разрешает конвертировать API format или менять semantic payload.
"""
    changed |= ensure_contains(
        path,
        block3,
        after="Request semantic payload не меняется.",
    )

    return changed


def patch_011(root: Path) -> bool:
    path = root / "docs/spec/011-native-api-families.ru.md"
    if path.exists() and path.read_text(encoding="utf-8") == NATIVE_API_SPEC:
        return False
    path.write_text(NATIVE_API_SPEC, encoding="utf-8")
    return True


def patch_030(root: Path) -> bool:
    path = root / "docs/spec/030-routing-and-resellers.ru.md"
    changed = False

    changed |= replace_if_present(
        path,
        "provider_model\nenabled",
        "provider_model\nmodel_rewrite_policy\nenabled",
    )

    # Replace provider_model section if it is still the original.
    if "## 5.5. Model rewrite policy" not in read(path):
        changed |= regex_replace_once(
            path,
            r"## 5\.4\. Provider model\n.*?(?=\n---\n\n# 6\. API family)",
            """## 5.4. Provider model

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
            flags=re.S,
        )

    changed |= replace_if_present(
        path,
        "capabilities = union of enabled routes for this client_model and endpoint type",
        "capabilities = intersection of available routes for this client_model and endpoint type",
    )

    block = """
Причина:

```text
/v1/models не должен обещать combination capabilities,
если ни один concrete route не может обслужить такой request.
```

Будущая версия может вернуть `capability_profiles[]`, но первая версия использует conservative intersection.
"""
    changed |= ensure_contains(
        path,
        block,
        after="capabilities = intersection of available routes for this client_model and endpoint type\n```",
    )

    return changed


def patch_040(root: Path) -> bool:
    path = root / "docs/spec/040-pricing-and-usage.ru.md"
    changed = False
    content = read(path)

    if "image_generation_units" not in content:
        # Add to first normalized usage code block by placing after video_input_tokens.
        content = content.replace(
            "video_input_tokens\n```",
            "video_input_tokens\nimage_generation_units\n```",
            1,
        )
        path.write_text(content, encoding="utf-8")
        changed = True

    block_usage = """
## 4.11. image_generation_units

`image_generation_units` — нормализованные billable units для `/v1/images/generations`.

Первая версия использует deterministic unit rule:

```text
image_generation_units = request.n OR 1
```

Provider-specific adapter может вернуть более точное значение, если upstream тарифицирует generation иначе.

Если route не имеет цены для image generation units и adapter не может вернуть token-equivalent usage, route считается invalid для `images_generation`.

"""
    changed |= ensure_contains(path, block_usage, before="\n---\n\n# 5. Route price catalog")

    changed |= replace_if_present(
        path,
        "video_input_price_per_1m_tokens_cents\nmarkup_coefficient",
        "video_input_price_per_1m_tokens_cents\nimage_generation_price_per_unit_cents\nimage_generation_unit_kind\nmarkup_coefficient",
    )

    block_formula = """
Image generation unit pricing is included in the same single-rounding formula.

Because token prices are stored as cents per 1,000,000 tokens and image generation unit price is already cents per unit, the unit component is normalized into raw cost as:

```text
image_generation_unit_raw =
  image_generation_units * image_generation_price_per_unit_cents * 1_000_000
```

Then:

```text
total_raw = token_category_raw_sum + image_generation_unit_raw
```

Final rounding still happens once at the end.
"""
    changed |= ensure_contains(path, block_formula, before="\n## 7.2. Final client amount")

    # Replace image generation section robustly.
    content = read(path)
    if "fixed unit pricing через image_generation_units" not in content:
        new_section = """## 11.3. Images generations

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
        content2, n = re.subn(
            r"## 11\.3\. Images generations\n.*?(?=\n---\n\n# 12\. Free or zero-cost usage)",
            new_section,
            content,
            count=1,
            flags=re.S,
        )
        if n == 0:
            fail("could not find section '## 11.3. Images generations' in docs/spec/040-pricing-and-usage.ru.md")
        changed |= write_if_changed(path, content2)

    changed |= replace_if_present(
        path,
        "video input pricing\nmultimodal max input rule",
        "video input pricing\nimage generation unit pricing\nmultimodal max input rule",
    )
    changed |= replace_if_present(
        path,
        "1. Usage нормализуется в фиксированные categories.",
        "1. Usage нормализуется в фиксированные categories, включая image_generation_units.",
    )

    return changed


def patch_060(root: Path) -> bool:
    path = root / "docs/spec/060-admin-api.ru.md"
    changed = False

    changed |= replace_if_present(
        path,
        '"provider_model": "openai/gpt-4.1-mini",\n  "enabled":',
        '"provider_model": "openai/gpt-4.1-mini",\n  "model_rewrite_policy": "provider_model",\n  "enabled":',
    )

    changed |= replace_if_present(
        path,
        "provider_model must be non-empty\ndefault_max_output_tokens required for chat routes",
        "provider_model must be non-empty\nmodel_rewrite_policy must be one of: none, provider_model\nif provider_model != client_model, model_rewrite_policy must be provider_model\ndefault_max_output_tokens required for chat routes",
    )

    changed |= replace_if_present(
        path,
        "provider_model\nenabled\npriority",
        "provider_model\nmodel_rewrite_policy\nenabled\npriority",
    )

    changed |= replace_if_present(
        path,
        '"video_input_price_per_1m_tokens_cents": 10000,\n    "markup_coefficient": 1.3,',
        '"video_input_price_per_1m_tokens_cents": 10000,\n    "image_generation_price_per_unit_cents": 150,\n    "image_generation_unit_kind": "generated_image",\n    "markup_coefficient": 1.3,',
    )

    changed |= replace_if_present(
        path,
        "markup_coefficient > 0\nroute_id must exist",
        "markup_coefficient > 0\nimage_generation_price_per_unit_cents >= 0\nimage_generation_unit_kind must be one of: none, generated_image\nroute_id must exist",
    )

    changed |= replace_if_present(
        path,
        "api_family allowed values\nendpoint_kind allowed values\ncurrency = RUB",
        "api_family allowed values\nendpoint_kind allowed values\nmodel_rewrite_policy allowed values\ncurrency = RUB",
    )

    return changed


def patch_070(root: Path) -> bool:
    path = root / "docs/spec/070-database-schema.ru.md"
    changed = False

    changed |= replace_if_present(
        path,
        "    client_model TEXT NOT NULL,\n    provider_model TEXT NOT NULL,\n\n    enabled BOOLEAN NOT NULL DEFAULT TRUE,",
        "    client_model TEXT NOT NULL,\n    provider_model TEXT NOT NULL,\n    model_rewrite_policy TEXT NOT NULL DEFAULT 'none',\n\n    enabled BOOLEAN NOT NULL DEFAULT TRUE,",
    )

    block_policy = """
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

"""
    changed |= ensure_contains(path, block_policy, before="\n## 8.5. Unique route identity")

    # Heading renumbering is optional, but keep it deterministic if original headings are still present.
    changed |= replace_if_present(path, "## 8.5. Unique route identity", "## 8.6. Unique route identity")
    changed |= replace_if_present(path, "## 8.6. Lookup index", "## 8.7. Lookup index")
    changed |= replace_if_present(path, "## 8.7. Cooldown index", "## 8.8. Cooldown index")
    changed |= replace_if_present(path, "## 8.8. Capabilities JSON", "## 8.9. Capabilities JSON")

    changed |= replace_if_present(
        path,
        "    video_input_price_per_1m_tokens_cents BIGINT NOT NULL DEFAULT 0,\n\n    markup_coefficient NUMERIC(12, 6) NOT NULL DEFAULT 1.0,",
        "    video_input_price_per_1m_tokens_cents BIGINT NOT NULL DEFAULT 0,\n\n    image_generation_price_per_unit_cents BIGINT NOT NULL DEFAULT 0,\n    image_generation_unit_kind TEXT NOT NULL DEFAULT 'none',\n\n    markup_coefficient NUMERIC(12, 6) NOT NULL DEFAULT 1.0,",
    )

    changed |= replace_if_present(
        path,
        "    CHECK (video_input_price_per_1m_tokens_cents >= 0),\n    CHECK (markup_coefficient > 0)",
        "    CHECK (video_input_price_per_1m_tokens_cents >= 0),\n    CHECK (image_generation_price_per_unit_cents >= 0),\n    CHECK (image_generation_unit_kind IN ('none', 'generated_image')),\n    CHECK (markup_coefficient > 0)",
    )

    changed |= replace_if_present(
        path,
        "    estimated_input_tokens BIGINT NOT NULL DEFAULT 0,\n    estimated_output_tokens BIGINT NOT NULL DEFAULT 0,\n    estimated_client_amount_cents BIGINT NOT NULL DEFAULT 0,",
        "    estimated_input_tokens BIGINT NOT NULL DEFAULT 0,\n    estimated_output_tokens BIGINT NOT NULL DEFAULT 0,\n    estimated_image_generation_units BIGINT NOT NULL DEFAULT 0,\n    estimated_client_amount_cents BIGINT NOT NULL DEFAULT 0,",
    )

    changed |= replace_if_present(
        path,
        "    video_input_tokens BIGINT NOT NULL DEFAULT 0,\n\n    client_amount_cents BIGINT NOT NULL DEFAULT 0,",
        "    video_input_tokens BIGINT NOT NULL DEFAULT 0,\n    image_generation_units BIGINT NOT NULL DEFAULT 0,\n\n    client_amount_cents BIGINT NOT NULL DEFAULT 0,",
    )

    changed |= replace_if_present(
        path,
        "    CHECK (estimated_input_tokens >= 0),\n    CHECK (estimated_output_tokens >= 0),\n    CHECK (estimated_client_amount_cents >= 0),",
        "    CHECK (estimated_input_tokens >= 0),\n    CHECK (estimated_output_tokens >= 0),\n    CHECK (estimated_image_generation_units >= 0),\n    CHECK (estimated_client_amount_cents >= 0),",
    )

    changed |= replace_if_present(
        path,
        "    CHECK (video_input_tokens >= 0),\n\n    CHECK (client_amount_cents >= 0),",
        "    CHECK (video_input_tokens >= 0),\n    CHECK (image_generation_units >= 0),\n\n    CHECK (client_amount_cents >= 0),",
    )

    changed |= replace_if_present(
        path,
        "8. Route prices support all token categories.",
        "8. Route prices support all token categories and image generation unit pricing.",
    )

    return changed


def main() -> int:
    root = repo_root()

    patchers = [
        ("docs/adr/0001-tokenio-gateway-architecture.ru.md", patch_adr),
        ("docs/spec/000-tokenio-gateway.ru.md", patch_000),
        ("docs/spec/010-external-api.ru.md", patch_010),
        ("docs/spec/011-native-api-families.ru.md", patch_011),
        ("docs/spec/030-routing-and-resellers.ru.md", patch_030),
        ("docs/spec/040-pricing-and-usage.ru.md", patch_040),
        ("docs/spec/060-admin-api.ru.md", patch_060),
        ("docs/spec/070-database-schema.ru.md", patch_070),
    ]

    changed_files: list[str] = []
    for rel, fn in patchers:
        if fn(root):
            changed_files.append(rel)

    print("Changed files:")
    if changed_files:
        for f in changed_files:
            print(f"  - {f}")
    else:
        print("  no changes; patch already applied")

    print("\n--- git diff ---")
    code, out = run(["git", "diff"])
    print(out)

    print("\n--- verification grep ---")
    patterns = [
        "model_rewrite_policy",
        "011-native-api-families",
        "intersection of available routes",
        "image_generation_price_per_unit_cents",
        "image_generation_units",
        "semantic request payload",
        "POST /v1/messages",
        "POST /v1beta/models/{model}:generateContent",
    ]
    for pattern in patterns:
        print(f"$ grep -R -n {pattern!r} docs/spec docs/adr")
        _, out = run(["grep", "-R", "-n", pattern, "docs/spec", "docs/adr"])
        print(out if out else f"NO MATCH: {pattern}")

    print("\nDone.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
