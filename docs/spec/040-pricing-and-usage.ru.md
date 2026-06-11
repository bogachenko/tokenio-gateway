# 040. Pricing and Usage

Версия: 0.1
Статус: draft
Язык: русский
Проект: `github.com/bogachenko/tokenio-gateway`

---

# 1. Назначение документа

Этот документ описывает pricing and usage accounting layer Tokenio Gateway.

Документ фиксирует:

```text
usage categories
usage extraction
local token estimation
preflight estimation
route price catalog
upstream cost calculation
client sell amount calculation
markup
rounding
multimodal pricing rule
billing headers
provider-specific usage adapter boundary
```

Документ не описывает:

```text
public HTTP API
API key validation
route selection algorithm
ledger state machine
billing auto-charge
admin API endpoints
database migrations
```

Эти темы описываются в отдельных спецификациях.

---

# 2. Главный pricing invariant

Tokenio Gateway считает стоимость по собственному pricing catalog.

Source of truth для цены:

```text
route_price
```

Provider response может использоваться как source of truth для token usage, если usage достаточно детальный.

Provider response не является универсальным source of truth для client billing amount.

Запрещено:

```text
считать client amount напрямую из provider-specific cost_request в generic layer
считать multimodal request только как text input, если есть image/audio/file/video
округлять стоимость на промежуточных этапах
подменять отсутствующий usage нулевой стоимостью
смешивать upstream cost и client amount
```

---

# 3. Currency

В первой версии поддерживается только:

```text
RUB
```

Все цены хранятся в:

```text
RUB cents
```

Если route price имеет currency не `RUB`, route считается invalid.

Ошибка configuration/admin validation:

```text
unsupported_currency
```

---

# 4. Usage categories

## 4.1. Нормализованная структура usage

Tokenio Gateway нормализует usage в структуру:

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
```

## 4.2. input_tokens

`input_tokens` — обычные текстовые input/prompt tokens.

Примеры:

```text
messages text
system prompt text
developer prompt text
plain user text
embedding input text
```

## 4.3. cached_input_tokens

`cached_input_tokens` — input tokens, которые provider считает cached.

Если provider возвращает cached token details, они должны быть выделены отдельно.

Если provider не возвращает cached details, значение:

```text
cached_input_tokens = 0
```

## 4.4. output_tokens

`output_tokens` — обычные completion/output tokens.

Примеры:

```text
assistant text output
tool call JSON output, если provider считает это output
embedding response does not normally use output_tokens
```

## 4.5. reasoning_tokens

`reasoning_tokens` — internal reasoning tokens, если provider возвращает их в usage.

Если provider не возвращает reasoning details:

```text
reasoning_tokens = 0
```

Запрещено угадывать reasoning tokens по тексту ответа.

## 4.6. image_input_tokens

`image_input_tokens` — input tokens или token-equivalent units, связанные с image input.

Примеры:

```text
image_url in chat
base64 image content
multimodal image part
```

Если provider возвращает image token breakdown, значение берётся из provider usage.

Если provider не возвращает breakdown, применяется multimodal max input rule.

## 4.7. audio_input_tokens

`audio_input_tokens` — input tokens или token-equivalent units, связанные с audio input.

Примеры:

```text
audio content in chat
audio_url
base64 audio
```

Если provider возвращает audio input breakdown, значение берётся из provider usage.

Если provider не возвращает breakdown, применяется multimodal max input rule.

## 4.8. audio_output_tokens

`audio_output_tokens` — output audio tokens или token-equivalent units, если provider возвращает audio output usage.

Если audio output есть, но provider usage неполный, pricing adapter обязан использовать conservative estimation.

## 4.9. file_input_tokens

`file_input_tokens` — input tokens или token-equivalent units, связанные с file/pdf/document input.

Примеры:

```text
pdf input
document input
file content in multimodal message
file reference, если provider тарифицирует его как input
```

Если provider возвращает file token breakdown, значение берётся из provider usage.

Если provider не возвращает breakdown, применяется multimodal max input rule.

## 4.10. video_input_tokens

`video_input_tokens` — input tokens или token-equivalent units, связанные с video input.

Если provider возвращает video token breakdown, значение берётся из provider usage.

Если provider не возвращает breakdown, применяется multimodal max input rule.



## 4.11. image_generation_units

`image_generation_units` — нормализованные billable units для `/v1/images/generations`.

Первая версия использует deterministic unit rule:

```text
image_generation_units = request.n OR 1
```

Provider-specific adapter может вернуть более точное значение, если upstream тарифицирует generation иначе.

Если route не имеет цены для image generation units и adapter не может вернуть token-equivalent usage, route считается invalid для `images_generation`.


---

# 5. Route price catalog

## 5.1. Route-specific prices

Цена задаётся на уровне route.

Минимальная структура:

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
enabled
created_at
updated_at
```

## 5.2. Price units

Все token prices задаются как:

```text
RUB cents per 1,000,000 tokens
```

Пример:

```text
input_price_per_1m_tokens_cents = 1000
```

означает:

```text
10 RUB за 1,000,000 input tokens
```

## 5.3. Missing price

Если route требует usage category, но цена для этой category отсутствует или равна нулю, route считается invalid для такого request, кроме случаев, где category реально не используется.

Пример:

```text
request содержит image input
route.image_input_price_per_1m_tokens_cents = 0
```

Тогда route нельзя использовать для image input, если только adapter явно не сообщает, что provider тарифицирует image input как обычный input.

Это должно быть зафиксировано в route metadata.

---

# 6. Markup

## 6.1. Markup coefficient

`markup_coefficient` задаётся на уровне route price.

Client amount считается так:

```text
client_amount_cents = ceil(upstream_cost_cents_raw * markup_coefficient)
```

Но фактически округление выполняется один раз после полного расчёта token cost.

## 6.2. Markup validation

`markup_coefficient` должен быть:

```text
> 0
```

Если markup отсутствует, default первой версии:

```text
1.0
```

## 6.3. Route selection and markup

Route selection выбирает самый дешёвый route по:

```text
estimated_upstream_cost_cents
```

Client billing amount считается уже после выбора route.

---

# 7. Cost calculation

## 7.1. Raw token cost

Для каждой usage category считается raw стоимость:

```text
category_cost_raw =
  token_count * price_per_1m_tokens_cents
```

Сумма raw costs:

```text
total_raw =
  input_tokens        * input_price_per_1m_tokens_cents
+ cached_input_tokens * cached_input_price_per_1m_tokens_cents
+ output_tokens       * output_price_per_1m_tokens_cents
+ reasoning_tokens    * reasoning_output_price_per_1m_tokens_cents
+ image_input_tokens  * image_input_price_per_1m_tokens_cents
+ audio_input_tokens  * audio_input_price_per_1m_tokens_cents
+ audio_output_tokens * audio_output_price_per_1m_tokens_cents
+ file_input_tokens   * file_input_price_per_1m_tokens_cents
+ video_input_tokens  * video_input_price_per_1m_tokens_cents
```

Так как prices заданы за 1M tokens, сумма в cents:

```text
amount_before_markup_cents =
  total_raw / 1_000_000
```

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


## 7.2. Final client amount

Финальный client amount:

```text
client_amount_cents =
  ceil((total_raw / 1_000_000) * markup_coefficient)
```

Округление выполняется один раз в конце.

## 7.3. Upstream cost

Upstream cost считается без markup:

```text
upstream_cost_cents =
  ceil(total_raw / 1_000_000)
```

Upstream cost используется для:

```text
route selection
reseller balance reserve
reseller balance reconciliation
profit calculation
```

Client amount используется для:

```text
user pending ledger
billing charge
response billing headers
```

## 7.4. Rounding

Запрещено:

```text
округлять каждую category отдельно
округлять до применения markup
округлять estimate и потом использовать его как actual
```

Разрешено:

```text
округлить final upstream_cost_cents
округлить final client_amount_cents
```

---

# 8. Usage extraction

## 8.1. Usage source priority

Usage определяется по приоритету:

```text
1. Provider detailed usage response.
2. Provider aggregate usage response + structural request modality detection.
3. Provider-specific request/response estimator.
4. Generic conservative estimator.
```

## 8.2. Detailed provider usage

Если provider возвращает детальный breakdown:

```text
input
cached_input
output
reasoning
image_input
audio_input
audio_output
file_input
video_input
```

gateway должен использовать этот breakdown.

## 8.3. Aggregate provider usage

Если provider возвращает только:

```text
input_tokens
output_tokens
```

и request является text-only, gateway может использовать:

```text
input_tokens -> input_tokens
output_tokens -> output_tokens
```

Если request содержит image/audio/file/video input, применяется multimodal max input rule.

## 8.4. Missing provider usage

Если provider не вернул usage, gateway не должен считать стоимость нулевой.

Gateway должен использовать local token estimator.

Если estimator не может безопасно оценить request, gateway должен вернуть ошибку до billable commit или использовать configured conservative minimum charge.

Решение первой версии:

```text
использовать conservative estimator;
если estimator не поддерживает данный endpoint/model/category, вернуть pricing_unavailable до upstream request, если это выявлено preflight;
если это выявлено после successful upstream, записать usage как pricing_failed и заблокировать повторное списание до manual/admin resolution.
```

## 8.5. Provider-specific usage extractors

Provider-specific extraction живёт в adapter layer.

Generic pricing layer не должен знать provider response schema.

Adapter возвращает нормализованный result:

```text
usage
usage_completeness
has_multimodal_input
has_aggregate_only_input
provider_request_id
provider_response_model
```

---

# 9. Multimodal max input rule

## 9.1. Проблема

Некоторые providers возвращают только общий `input_tokens`, без разбивки на:

```text
text
image
audio
file
video
```

Если request содержит multimodal input, а gateway посчитает весь input как text input, стоимость может быть занижена.

## 9.2. Правило

Если выполняются условия:

```text
request contains image/audio/file/video input
provider returned only aggregate input_tokens
provider did not return modality breakdown
```

то gateway должен считать весь aggregate input по самой дорогой input category среди присутствующих modalities.

Формально:

```text
max_input_price =
  max(
    input_price_per_1m_tokens_cents,
    image_input_price_per_1m_tokens_cents if image present,
    audio_input_price_per_1m_tokens_cents if audio present,
    file_input_price_per_1m_tokens_cents if file present,
    video_input_price_per_1m_tokens_cents if video present
  )
```

Стоимость:

```text
aggregate_input_cost_raw =
  provider_input_tokens * max_input_price
```

## 9.3. Examples

### Text-only request

Request:

```text
text only
```

Provider usage:

```text
input_tokens = 1000
output_tokens = 500
```

Pricing:

```text
1000 * input_price
500  * output_price
```

### Image input request, aggregate usage only

Request:

```text
text + image
```

Provider usage:

```text
input_tokens = 1000
output_tokens = 500
```

Prices:

```text
input_price = 1000
image_input_price = 4000
```

Pricing:

```text
1000 * 4000
500  * output_price
```

### Audio + file request, aggregate usage only

Request:

```text
text + audio + file
```

Provider usage:

```text
input_tokens = 2000
```

Prices:

```text
input_price = 1000
audio_input_price = 3000
file_input_price = 5000
```

Pricing:

```text
2000 * 5000
```

## 9.4. No semantic modality detection

Modality detection должна быть structural.

Запрещено:

```text
читать prompt text и пытаться понять, есть ли там изображение
искать слова "image", "audio", "pdf" в обычном тексте
принимать pricing decision на основе natural language content
```

Разрешено:

```text
смотреть JSON structure request body
смотреть content part type
смотреть file/image/audio/video fields
смотреть mime type, если он явно структурно передан
```

---

# 10. Preflight estimation

## 10.1. Purpose

Preflight estimation нужна до upstream request для:

```text
проверки user effective balance
выбора cheapest route
проверки reseller balance
создания reseller reserve
создания local usage reserve
```

## 10.2. Inputs

Estimator получает:

```text
api_family
endpoint_kind
client_model
request body
route default_max_output_tokens
route price
requested capabilities
```

## 10.3. Output

Estimator возвращает:

```text
estimated_usage
estimated_upstream_cost_cents
estimated_client_amount_cents
estimation_confidence
```

## 10.4. Safety coefficient

Preflight estimate должен быть conservative.

Config keys:

```text
TOKENIO_TOKEN_ESTIMATION_SAFETY_FACTOR
TOKENIO_COST_ESTIMATION_SAFETY_FACTOR
```

Default:

```text
TOKENIO_TOKEN_ESTIMATION_SAFETY_FACTOR=1.25
TOKENIO_COST_ESTIMATION_SAFETY_FACTOR=1.10
```

Если local tokenizer/estimator не уверен, он обязан завысить оценку, а не занизить.

## 10.5. max_tokens absent

Если client request не содержит explicit max output limit, estimator использует:

```text
route.default_max_output_tokens
```

Если `default_max_output_tokens` отсутствует или равен `0`, route считается invalid для preflight.

Ошибка:

```text
pricing_unavailable
```

## 10.6. Request body mutation forbidden

Estimator не должен добавлять `max_tokens` в request body.

Даже если estimator использует `default_max_output_tokens`, request body upstream отправляется без изменений.

---

# 11. Endpoint-specific usage

## 11.1. Chat completions

Для `/v1/chat/completions` учитываются:

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
```

`output_tokens` может быть `0`, если provider вернул tool-only response и считает usage иначе.

## 11.2. Embeddings

Для `/v1/embeddings` обычно используются:

```text
input_tokens
```

`output_tokens` обычно равен:

```text
0
```

Если provider возвращает `total_tokens` без `input_tokens`, для embeddings допускается:

```text
input_tokens = total_tokens
```

Это правило должно быть реализовано в endpoint-specific usage extractor, не в generic pricing formula.

## 11.3. Images generations

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


---

# 12. Free or zero-cost usage

## 12.1. Free upstream request

Если provider сообщает, что upstream request был free, gateway всё равно должен записать usage.

Client billing policy первой версии:

```text
если route price и usage дают amount 0, client_amount_cents = 0
если provider-specific free flag есть, но route price catalog говорит цену > 0, source of truth = route price catalog
```

Иными словами, provider free flag не должен автоматически делать client request бесплатным.

## 12.2. Zero tokens

Если provider вернул нулевой usage для successful response, gateway должен проверить, допустимо ли это для endpoint.

Если нулевой usage выглядит невозможным, gateway должен применить conservative estimator или manual review policy.

Запрещено silently списывать `0`, если usage отсутствует из-за parsing failure.

---

# 13. Usage completeness

Adapter должен помечать completeness:

```text
detailed
aggregate
estimated
missing
failed
```

## 13.1. detailed

Provider вернул полный breakdown по relevant categories.

## 13.2. aggregate

Provider вернул только общий input/output без modality breakdown.

## 13.3. estimated

Usage был рассчитан локально estimator-ом.

## 13.4. missing

Provider usage отсутствует, estimator ещё не применён.

## 13.5. failed

Usage extraction или estimation failed.

Если status `failed`, usage не должен быть committed как charged без admin/manual resolution.

---

# 14. Response billing headers

После успешного billable calculation gateway возвращает billing headers.

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
```

Headers должны отражать actual committed usage, а не preflight estimate, если actual usage доступен.

Если usage estimated после response, headers должны отражать estimated usage и amount.

---

# 15. Billing model string

Для billing service поле `model` формируется как:

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

`provider_type` берётся из фактически выбранного route.

Если fallback route был использован, billing model string должен отражать fallback route provider_type.

---

# 16. Provider-specific cost fields

Некоторые providers могут возвращать готовую стоимость request.

Примеры provider-specific fields:

```text
cost_request
free_request
total_cost
billable_units
```

Generic pricing layer не должен напрямую зависеть от этих полей.

Provider adapter может использовать такие поля для:

```text
debug
upstream cost reconciliation
sanity check
provider balance estimation
```

Client billing amount первой версии считается по route price catalog.

Если provider-specific cost сильно расходится с calculated upstream cost, gateway должен записать diagnostic event для admin/debug.

---

# 17. Profit and reseller accounting

Для каждого usage record gateway должен хранить:

```text
estimated_upstream_cost_cents
actual_upstream_cost_cents
estimated_client_amount_cents
client_amount_cents
currency
```

Profit может быть рассчитан как:

```text
client_amount_cents - actual_upstream_cost_cents
```

Profit не должен влиять на response клиенту.

---

# 18. Pricing failure

## 18.1. Failure before upstream

Если pricing unavailable выявлен до upstream request:

```text
HTTP 503
error.code = pricing_unavailable
```

Request не должен быть отправлен upstream.

## 18.2. Failure after upstream success

Если upstream request successful, но usage/pricing failed:

```text
response body клиенту может быть возвращён
usage record должен быть сохранён как pricing_failed или failed
future requests must account for unresolved pending risk according to ledger spec
```

Решение первой версии:

```text
если upstream success, но pricing failed, gateway возвращает upstream response body,
добавляет X-Billing-Status: pricing_failed,
сохраняет usage record для manual/admin resolution,
не делает автоматическое списание до resolution.
```

Это состояние должно быть видно в admin API.

---

# 19. Tests

Pricing and usage layer должен иметь unit tests для:

```text
text-only input/output pricing
cached input pricing
reasoning output pricing
image input pricing
audio input pricing
audio output pricing
file input pricing
video input pricing
image generation unit pricing
multimodal max input rule
single final rounding
markup application
zero usage
missing price
unknown currency
preflight safety coefficient
max_tokens absent uses route.default_max_output_tokens
provider detailed usage priority
provider aggregate usage with multimodal input
billing model string provider_type:client_model
```

---

# 20. Acceptance criteria

Pricing and usage layer считается реализованным, если:

```text
1. Usage нормализуется в фиксированные categories, включая image_generation_units.
2. Client amount считается из route price catalog.
3. Currency первой версии только RUB.
4. Округление выполняется один раз в конце.
5. Markup применяется после суммирования raw token costs.
6. Upstream cost отделён от client amount.
7. Provider-specific cost_request не является generic source of truth.
8. Missing usage не приводит к нулевому billing.
9. Text-only aggregate usage считается как text input/output.
10. Multimodal aggregate usage считается по самой дорогой присутствующей input category.
11. Preflight estimation использует safety coefficients.
12. max_tokens absent использует route.default_max_output_tokens.
13. Request body не мутируется для estimate.
14. Billing headers отражают committed actual или estimated usage.
15. Pricing failure after upstream success не теряет usage и виден admin/debug layer.
16. Tests покрывают все token categories и multimodal max input rule.
```
