# 080. Error Model

Версия: 0.1
Статус: draft
Язык: русский
Проект: `github.com/bogachenko/tokenio-gateway`

---

# 1. Назначение документа

Этот документ описывает error model Tokenio Gateway.

Документ фиксирует:

```text
gateway-owned error envelope
HTTP status mapping
public error codes
admin error codes
upstream error passthrough
request id behavior
billing error behavior
routing error behavior
ledger error behavior
secrets policy for errors
logging policy
```

Документ не описывает:

```text
route selection algorithm
pricing formula
ledger state machine
admin endpoint definitions
database schema
provider adapter implementation
```

Эти темы описываются в отдельных спецификациях.

---

# 2. Главный error invariant

Tokenio Gateway различает два типа upstream results:

```text
1. Successful upstream responses
2. Upstream/provider failures
```

Successful upstream responses возвращаются клиенту без изменения response body.

Upstream/provider failures не возвращаются клиенту как raw provider response body. Они классифицируются provider adapter layer и возвращаются как gateway-owned errors в едином JSON envelope.

Запрещено:

```text id="9wub64"
возвращать raw provider error body клиенту
возвращать raw upstream 4xx body клиенту
возвращать raw upstream 5xx body клиенту
смешивать gateway validation error и provider error без normalized code
переписывать successful upstream response body
возвращать raw Go error клиенту
возвращать secrets в error message
возвращать stack trace клиенту
раскрывать api_key_env обычному клиенту
раскрывать reseller_id обычному клиенту, если это не public/debug contract
```

Разрешено:

```text id="x8o11m"
возвращать successful upstream response body без изменений
добавлять Tokenio billing/debug headers к successful response
использовать provider adapter classifier для выбора gateway-owned error.code
логировать safe classifier metadata без raw provider error body
```

Правило:

```text id="ax8pfl"
success body passthrough applies only to successful upstream responses.
provider/upstream errors are normalized by Tokenio Gateway.
```

---

# 3. Gateway-owned error envelope

Все gateway-owned errors возвращаются так:

```json
{
  "error": {
    "code": "unknown_model",
    "message": "Unknown model",
    "request_id": "llmreq_..."
  }
}
```

Поля:

```text
error.code       — стабильный machine-readable код
error.message    — человекочитаемое сообщение
error.request_id — local request id или admin request id, если уже создан
```

## 3.1. Required fields

`code` обязателен.

`message` обязателен.

`request_id` обязателен, если request id уже создан.

## 3.2. Optional fields

Первая версия не добавляет дополнительные поля в public error envelope.

Будущие версии могут добавить:

```text
details
param
type
retry_after_seconds
```

Но это должно быть отдельным contract update.

---

# 4. Request IDs in errors

## 4.1. Public LLM request id

Для LLM endpoints используется:

```text
local_request_id = llmreq_<random>
```

Он должен возвращаться в:

```http
X-Local-Request-ID: llmreq_...
```

И в body:

```json
{
  "error": {
    "request_id": "llmreq_..."
  }
}
```

если request id уже создан.

## 4.2. Admin request id

Для admin endpoints используется:

```text
admin_request_id = admreq_<random>
```

Он возвращается в admin error envelope.

## 4.3. Errors before request id

Если ошибка произошла до создания request id, `request_id` может отсутствовать.

Рекомендуемое поведение:

```text
создавать request id как можно раньше на boundary handler
```

---

# 5. HTTP status classes

## 5.1. 4xx

4xx используется, если проблема вызвана request/client/account state.

Примеры:

```text
missing auth
invalid API key
unknown model
unsupported capability
invalid JSON
insufficient funds
idempotency conflict
```

## 5.2. 5xx

5xx используется, если gateway не может выполнить request из-за runtime/dependency/unavailable state.

Примеры:

```text
no available route
billing service unavailable
usage store unavailable
pricing unavailable
upstream unavailable before response
internal error
```

## 5.3. 502 vs 503

`502 Bad Gateway` используется, если конкретная dependency ответила некорректно или недоступна в момент вызова.

`503 Service Unavailable` используется, если gateway не может выбрать/использовать подходящий route или временно не готов обслужить request.

---

# 6. Auth errors

## 6.1. Missing Authorization

```text
HTTP 401
error.code = unauthorized
error.message = Authorization header is required
```

## 6.2. Invalid Authorization format

```text
HTTP 401
error.code = unauthorized
error.message = Authorization header format must be Bearer {api_key}
```

## 6.3. Invalid API key prefix

```text
HTTP 401
error.code = unauthorized
error.message = API key must start with sk_
```

## 6.4. Unknown/disabled/revoked/expired API key

```text
HTTP 401
error.code = invalid_api_key
error.message = Invalid API key
```

Gateway не должен раскрывать, key:

```text
не найден
отключён
отозван
истёк
```

## 6.5. Disabled user

```text
HTTP 403
error.code = user_disabled
error.message = User is disabled
```

---

# 7. Request validation errors

## 7.1. Invalid JSON

```text
HTTP 400
error.code = invalid_json
error.message = Invalid JSON request body
```

## 7.2. Request body too large

```text
HTTP 413
error.code = request_body_too_large
error.message = Request body is too large
```

## 7.3. Model required

```text
HTTP 400
error.code = model_required
error.message = model is required
```

## 7.4. Streaming unsupported

```text
HTTP 400
error.code = streaming_unsupported
error.message = Streaming is not supported
```

Gateway не должен silently downgrade streaming request.

## 7.5. Unsupported content type

Первая версия может пытаться обработать отсутствующий `Content-Type` как JSON.

Если `Content-Type` явно несовместим:

```text
HTTP 415
error.code = unsupported_content_type
error.message = Content-Type must be application/json
```

---

# 8. Routing errors

## 8.1. Unknown model

Если route registry не содержит ни одного route для:

```text
api_family + endpoint_kind + client_model
```

ответ:

```text
HTTP 400
error.code = unknown_model
error.message = Unknown model
```

## 8.2. Unsupported capability

Если model существует, но все routes исключены из-за requested capabilities:

```text
HTTP 400
error.code = unsupported_capability
error.message = Requested capability is not supported by this model
```

Public error не должен раскрывать internal route IDs.

## 8.3. No route available

Если routes существуют, но все недоступны из-за cooldown, balance, limits, missing reseller API key или disabled state:

```text
HTTP 503
error.code = no_route_available
error.message = No route is currently available for this model
```

Public error не должен раскрывать:

```text
reseller_id
api_key_env
internal balance
cooldown reason
provider API key state
```

Admin/debug API может показывать подробности.

## 8.4. Route unavailable during execution

Если выбранный route стал unavailable до upstream request:

```text
HTTP 503
error.code = route_unavailable
error.message = Selected route is unavailable
```

Если есть safe retry candidate, gateway должен попробовать следующий совместимый route вместо немедленного возврата ошибки.

---

# 9. Balance and billing errors

## 9.1. Insufficient funds

Если user effective balance недостаточен:

```text
HTTP 402
error.code = insufficient_funds
error.message = Insufficient funds
```

## 9.2. Billing unavailable before upstream

Если billing service недоступен до upstream request, и gateway не может принять решение по cached effective balance:

```text
HTTP 502
error.code = billing_unavailable
error.message = Billing service is unavailable
```

Request не должен быть отправлен upstream.

## 9.3. Billing unavailable after successful upstream

Если upstream request successful, usage committed as billable, но auto-charge failed:

```text
HTTP response status = upstream successful status
response body = upstream response body unchanged
X-Billing-Auto-Charge-Status: failed
```

Gateway не должен превращать successful upstream response в 502 только из-за failed auto-charge.

Usage остаётся pending в ledger.

## 9.4. Auto-charge failed

Если auto-charge failed после billable commit:

```text
X-Billing-Auto-Charge-Status: failed
```

Это не gateway-owned response error для клиента, если upstream response successful.

Admin API должен показывать failed charge diagnostics.

---

# 10. Pricing and usage errors

## 10.1. Pricing unavailable before upstream

Если route price или estimator недоступен до upstream request:

```text
HTTP 503
error.code = pricing_unavailable
error.message = Pricing is unavailable for this request
```

Request не должен быть отправлен upstream.

## 10.2. Usage extraction failed after upstream success

Если upstream successful, но usage extraction/pricing failed:

```text
HTTP response status = upstream successful status
response body = upstream response body unchanged
X-Billing-Status: pricing_failed
```

Usage record сохраняется в статусе:

```text
pricing_failed
```

Auto-charge не запускается.

Новые LLM requests пользователя блокируются до manual resolution согласно ledger policy.

## 10.3. Unresolved usage

Если у пользователя есть unresolved `pricing_failed` records:

```text
HTTP 409
error.code = unresolved_usage
error.message = User has unresolved usage records
```

---

# 11. Ledger and idempotency errors

## 11.1. Request in progress

Если повторный request с тем же idempotency scope пришёл, когда existing record имеет статус `reserved`:

```text
HTTP 409
error.code = request_in_progress
error.message = Request is already in progress
```

## 11.2. Idempotency replay not available

Если existing record уже `billable`, `partially_charged` или `charged`, но gateway первой версии не хранит full upstream response body:

```text
HTTP 409
error.code = idempotency_replay_not_available
error.message = Idempotency replay is not available
```

Важно:

```text
второй billable usage не создаётся
повторное списание не выполняется
```

## 11.3. Idempotency key reused

Если existing record `failed` или `released`, а policy первой версии запрещает reuse:

```text
HTTP 409
error.code = idempotency_key_reused
error.message = Idempotency key was already used
```

## 11.4. Usage store error

Если Postgres недоступен до upstream request:

```text
HTTP 503
error.code = usage_store_unavailable
error.message = Usage store is unavailable
```

Request не должен быть отправлен upstream, если gateway не может создать local reserve.

Если Postgres недоступен после upstream success, gateway должен пытаться сохранить usage; если сохранить невозможно, это critical incident.

Policy первой версии:

```text
если ledger commit невозможен после upstream success,
gateway возвращает upstream response body,
добавляет X-Billing-Status: ledger_commit_failed,
логирует critical error,
и блокирует дальнейшую обработку до восстановления storage policy.
```

---

# 12. Upstream/provider failures

## 12.1. Main rule

Если upstream вернул successful response и gateway не выполняет retry:

```text id="pu8byn"
HTTP status = upstream successful status
response body = upstream response body unchanged
```

Если upstream вернул error response или произошла upstream failure:

```text id="hj3jhq"
provider adapter classifies the failure
gateway returns gateway-owned error envelope
raw provider error body is not returned to public client
```

Gateway может добавить безопасные Tokenio headers.

## 12.2. Upstream 4xx

Если upstream вернул deterministic 4xx, связанный с client request, и retry запрещён:

```text id="lk6twu"
HTTP 400
error.code = upstream_request_error
error.message = Upstream rejected the request
```

Gateway не должен возвращать raw upstream error body клиенту.

Gateway не должен превращать upstream request error в собственный validation error вроде `invalid_json` или `unknown_model`, если request уже был отправлен upstream.

## 12.3. Upstream 401/403

Если upstream вернул auth-related 401/403 для reseller credential:

```text id="dy54vd"
adapter classifier = auth_error
route cooldown reason = auth_error
```

Если есть compatible retry candidate:

```text id="mpnjns"
try next compatible route
```

Если candidates закончились:

```text id="vtw40i"
HTTP 503
error.code = no_route_available
```

Public response не должен раскрывать reseller credential state, route_id, reseller_id или api_key_env.

## 12.4. Upstream 429

Если upstream вернул 429 и есть compatible retry candidate:

```text id="mba61g"
try next compatible route
```

Если candidates закончились:

```text id="ikggb2"
HTTP 503
error.code = no_route_available
```

Gateway не должен возвращать raw upstream 429 body клиенту.

## 12.5. Upstream quota or reseller balance error

Если provider adapter классифицировал upstream response как:

```text id="fm03gu"
quota_exceeded
insufficient_reseller_balance
```

gateway должен:

```text id="8sxkut"
1. mark route cooldown according to classifier
2. try next compatible route if retry boundary allows
3. return 503 no_route_available if candidates exhausted
```

Gateway не должен возвращать raw upstream body клиенту.

## 12.6. Upstream 5xx

Если upstream 5xx получен до response passthrough и есть compatible retry candidate:

```text id="bd1zgz"
try next compatible route
```

Если candidates закончились:

```text id="vlgs2a"
HTTP 503
error.code = no_route_available
```

Gateway не должен возвращать raw upstream 5xx body клиенту.

## 12.7. Upstream connection error before response

Если connection/DNS/TLS/timeout произошёл до response headers:

```text id="cak642"
try next compatible route if available
```

Если compatible route нет:

```text id="12t7rn"
HTTP 502
error.code = upstream_unavailable
error.message = Upstream is unavailable
```

## 12.8. Unsafe retry boundary

Если есть риск, что upstream начал обработку request, retry запрещён.

Если при этом upstream failure должна быть возвращена клиенту:

```text id="h5iad0"
gateway returns gateway-owned normalized error
raw provider error body is not returned
```

Если response headers already received:

```text id="ot637u"
retry forbidden
```

Если response was successful:

```text id="c0ohdu"
successful response body passthrough applies
```

Если response was error:

```text id="b65o8s"
provider error normalization applies
```

---

# 13. Method and path errors

## 13.1. Method not allowed

Если endpoint существует, но method неверный:

```text
HTTP 405
error.code = method_not_allowed
error.message = Method is not allowed
```

Response должен включать header:

```http
Allow: <allowed_methods>
```

## 13.2. Not found

Если endpoint неизвестен:

```text
HTTP 404
error.code = not_found
error.message = Endpoint not found
```

---

# 14. Admin errors

## 14.1. Admin unauthorized

```text
HTTP 401
error.code = admin_unauthorized
error.message = Admin authorization is required
```

## 14.2. Admin forbidden

```text
HTTP 403
error.code = admin_forbidden
error.message = Admin access denied
```

## 14.3. Admin validation error

```text
HTTP 400
error.code = admin_validation_error
error.message = Invalid admin request
```

## 14.4. Admin not found

```text
HTTP 404
error.code = admin_not_found
error.message = Resource not found
```

## 14.5. Admin conflict

```text
HTTP 409
error.code = admin_conflict
error.message = Resource conflict
```

## 14.6. Admin state conflict

```text
HTTP 409
error.code = admin_state_conflict
error.message = Resource is in incompatible state
```

## 14.7. Admin secret not available

Если admin API проверяет env presence для reseller credential:

```text
HTTP 409
error.code = admin_secret_not_available
error.message = Required secret is not available in environment
```

Error не должен раскрывать secret value.

---

# 15. Internal errors

## 15.1. Internal error

Для непредвиденной ошибки gateway:

```text
HTTP 500
error.code = internal_error
error.message = Internal error
```

Public response не должен содержать:

```text
panic message
stack trace
SQL query with secrets
raw upstream credentials
raw request body
```

## 15.2. Store unavailable

Если Postgres недоступен до upstream request:

```text
HTTP 503
error.code = store_unavailable
error.message = Store is unavailable
```

## 15.3. Configuration error

Runtime configuration errors should fail fast at startup when possible.

Если ошибка конфигурации обнаружена во время request:

```text
HTTP 500
error.code = configuration_error
error.message = Gateway configuration error
```

---

# 16. Retry-After

Gateway может возвращать:

```http
Retry-After: <seconds>
```

Для:

```text
no_route_available
billing_unavailable
upstream_unavailable
store_unavailable
```

`Retry-After` не обязателен в первой версии.

Если retry delay известен из cooldown или upstream header, gateway может использовать safe rounded value.

---

# 17. Headers on errors

Gateway-owned error response должен включать:

```http
Content-Type: application/json
X-Local-Request-ID: llmreq_...
```

если local request id создан.

Admin error response должен включать:

```http
Content-Type: application/json
X-Admin-Request-ID: admreq_...
```

если admin request id создан.

---

# 18. Secrets policy

Error response никогда не должен содержать:

```text
raw user API key
encrypted provisioning raw key
provisioning encryption nonce
provisioning encryption key
provisioning service token
key_hash
billing JWT
billing signing key
billing service token
reseller API key
admin token
Authorization header
X-Service-Token header
raw env secret value
stack trace
```

Public client error response также не должен содержать:

```text
api_key_env
reseller_id
route_id
internal balance
provider credential state
SQL details
```

Admin API может показывать:

```text
route_id
reseller_id
api_key_env
api_key_env_present
cooldown_reason
last_error_code
```

Но не secret values.

---

# 19. Logging

## 19.1. Log allowed fields

Gateway может логировать:

```text
request_id
user_id
api_key_id
api_family
endpoint_kind
client_model
provider_type
selected_route_id
selected_reseller_id
error_code
http_status
usage_status
billing_charge_request_id
duration_ms
```

## 19.2. Log forbidden fields

Gateway не должен логировать:

```text
raw API keys
Authorization header
billing JWT
billing service token
reseller API key
admin token
full request body by default
full response body by default
```

## 19.3. Upstream error body logging

Upstream error body может содержать sensitive data.

Default policy:

```text
do not log full upstream error body
```

Разрешено логировать:

```text
upstream status code
provider_type
route_id
safe error classifier code
body length
body hash
```

---

# 20. Public error code registry

Public LLM API error codes первой версии:

```text
unauthorized
invalid_api_key
user_disabled

invalid_json
request_body_too_large
unsupported_content_type
model_required
streaming_unsupported

unknown_model
unsupported_capability
no_route_available
route_unavailable

insufficient_funds
billing_unavailable

pricing_unavailable
unresolved_usage

request_in_progress
idempotency_replay_not_available
idempotency_key_reused

usage_store_unavailable
store_unavailable
upstream_request_error
upstream_unavailable
configuration_error
method_not_allowed
not_found
internal_error
```

---

# 21. Admin error code registry

Admin API error codes первой версии:

```text
admin_unauthorized
admin_forbidden
admin_validation_error
admin_not_found
admin_conflict
admin_state_conflict
admin_secret_not_available

invalid_json
request_body_too_large
method_not_allowed
not_found
store_unavailable
internal_error
```

---

# 22. Status mapping table

```text
unauthorized                          -> 401
invalid_api_key                       -> 401
user_disabled                         -> 403

invalid_json                          -> 400
request_body_too_large                -> 413
unsupported_content_type              -> 415
model_required                        -> 400
streaming_unsupported                 -> 400

unknown_model                         -> 400
unsupported_capability                -> 400
no_route_available                    -> 503
route_unavailable                     -> 503

insufficient_funds                    -> 402
billing_unavailable                   -> 502

pricing_unavailable                   -> 503
unresolved_usage                      -> 409

request_in_progress                   -> 409
idempotency_replay_not_available      -> 409
idempotency_key_reused                -> 409

usage_store_unavailable               -> 503
store_unavailable                     -> 503
upstream_request_error                -> 400
upstream_unavailable                  -> 502
configuration_error                   -> 500
method_not_allowed                    -> 405
not_found                             -> 404
internal_error                        -> 500

provisioning_unauthorized             -> 401
provisioning_invalid_request           -> 400
provisioning_conflict                  -> 409
provisioning_expired                   -> 410
provisioning_store_unavailable         -> 503
provisioning_crypto_unavailable        -> 500

admin_unauthorized                    -> 401
admin_forbidden                       -> 403
admin_validation_error                -> 400
admin_not_found                       -> 404
admin_conflict                        -> 409
admin_state_conflict                  -> 409
admin_secret_not_available            -> 409
```

---

# 23. Internal API key provisioning errors

Provisioning API является internal service API и использует тот же gateway-owned error envelope.

## 23.1. Unauthorized provisioning caller

```text
HTTP 401
error.code = provisioning_unauthorized
error.message = Provisioning authorization failed
```

Response не различает missing и invalid service token.

## 23.2. Invalid provisioning request

```text
HTTP 400
error.code = provisioning_invalid_request
error.message = Invalid provisioning request
```

Используется для missing/invalid:

```text
Idempotency-Key
external_billing_user_id
source_reference
```

## 23.3. Provisioning conflict

Если same `Idempotency-Key` повторён с другим normalized identity/source input:

```text
HTTP 409
error.code = provisioning_conflict
error.message = Provisioning request conflicts with existing state
```

## 23.4. Provisioning expired

Если provisioning delivery window expired:

```text
HTTP 410
error.code = provisioning_expired
error.message = Provisioning delivery window has expired
```

Raw key не возвращается.

## 23.5. Provisioning store unavailable

Если transaction/state store недоступен:

```text
HTTP 503
error.code = provisioning_store_unavailable
error.message = Provisioning store is unavailable
```

Новый raw key не возвращается как successful result без committed state.

## 23.6. Provisioning crypto unavailable

Если encryption key invalid, AEAD initialization failed или encrypted state нельзя безопасно расшифровать:

```text
HTTP 500
error.code = provisioning_crypto_unavailable
error.message = Provisioning encryption is unavailable
```

Response не содержит crypto details, ciphertext, nonce или key version secret value.

## 23.7. Registry

```text
provisioning_unauthorized
provisioning_invalid_request
provisioning_conflict
provisioning_expired
provisioning_store_unavailable
provisioning_crypto_unavailable
```

Detailed flow:

```text
docs/spec/021-api-key-provisioning.ru.md
```

# 24. Provider adapter error classifier

Provider-specific error parsing живёт в provider adapter layer.

Adapter возвращает normalized classifier:

```text
rate_limit
quota_exceeded
insufficient_reseller_balance
auth_error
provider_5xx
timeout
connection_error
client_request_error
unknown_upstream_error
```

Generic runtime использует classifier для:

```text
retry decision
cooldown reason
route event
public error mapping
```

Generic runtime не должен парсить provider-specific JSON schemas.

---

# 25. Acceptance criteria

Error model считается реализованным, если:

```text
1. Все gateway-owned errors используют единый JSON envelope.
2. Все gateway-owned errors имеют stable error.code.
3. request_id возвращается, если уже создан.
4. Auth errors не раскрывают детали key state.
5. Routing errors не раскрывают reseller secrets.
6. Billing unavailable before upstream возвращает gateway-owned error.
7. Billing failure after successful upstream не ломает upstream response.
8. Upstream successful response body не изменяется.
9. Upstream/provider failures нормализуются в gateway-owned error envelope и не возвращают raw provider body.
10. Retryable upstream failures классифицируются adapter layer.
11. Provider-specific parsing не находится в generic error writer.
12. Public errors не содержат secrets.
13. Admin errors используют отдельные admin_* codes.
14. Method not allowed возвращает 405 и Allow header.
15. Tests покрывают auth, validation, routing, billing, pricing, ledger, upstream passthrough и secrets policy.
16. Provisioning errors не раскрывают raw/encrypted API key material.
```
