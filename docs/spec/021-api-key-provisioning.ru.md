# 021. API Key Provisioning and Retry-Safe Delivery

Версия: 0.1
Статус: draft
Язык: русский
Проект: `github.com/bogachenko/tokenio-gateway`

---

# 1. Назначение документа

Этот документ описывает выпуск и retry-safe доставку пользовательского Tokenio API key после подтверждённой оплаты во внешнем Billing или другом доверенном payment channel.

Документ фиксирует:

```text
internal provisioning API
service-to-service authentication
external_billing_user_id mapping
idempotent API key creation
temporary encrypted raw key storage
delivery confirmation
provisioning expiration
subsequent top-up behavior
admin visibility
secrets and logging policy
```

Документ не описывает:

```text
payment provider integration
Telegram Bot API integration
billing wallet implementation
public LLM authentication
API key validation on /v1 endpoints
admin API key creation flow
```

---

# 2. Проблема доставки

Постоянное auth-хранилище Tokenio содержит только:

```text
HMAC-SHA256(TOKENIO_API_KEY_HASH_SECRET, raw_api_key)
```

Из HMAC невозможно восстановить raw API key.

Поэтому следующий flow некорректен:

```text
1. создать sk_...
2. сохранить только HMAC
3. вернуть raw key в HTTP response
4. навсегда забыть raw key
```

Если response потерян, повторный request не может вернуть тот же key.

Одновременно гарантировать:

```text
HMAC-only permanent auth storage
same-key idempotent retry
retry-safe delivery
```

можно только при наличии отдельного временного encrypted delivery state.

---

# 3. Главный provisioning invariant

Постоянный auth record:

```text
tokenio_api_keys
```

хранит только HMAC hash и display prefix.

Временный delivery record:

```text
tokenio_api_key_provisionings
```

может хранить reversible encrypted copy raw API key только пока:

```text
status = pending_delivery
```

После delivery confirmation или expiration:

```text
encrypted_raw_key = NULL
encryption_nonce = NULL
```

Raw API key:

```text
никогда не хранится plaintext
никогда не логируется
никогда не попадает в audit before_state/after_state
никогда не возвращается после delivery confirmation
```

---

# 4. Trust boundary

Provisioning API является internal service-to-service API.

Допустимые callers первой версии:

```text
Billing Service
Telegram payment bot backend
другой доверенный payment orchestrator
```

Caller обязан подтвердить оплату до вызова Tokenio.

Tokenio Gateway не принимает публичное утверждение пользователя о том, что платёж успешен.

Provisioning auth:

```http
X-Service-Token: <TOKENIO_PROVISIONING_SERVICE_TOKEN>
```

Запрещено использовать для provisioning:

```text
user sk_...
billing JWT
reseller API key
admin token
TOKENIO_BILLING_SERVICE_TOKEN
```

Provisioning service token является отдельным credential и отдельным trust boundary.

---

# 5. Identity mapping

Caller передаёт:

```text
external_billing_user_id
```

Tokenio выполняет:

```text
external_billing_user_id
-> tokenio_users.external_billing_user_id
-> tokenio_users.id
-> tokenio_api_keys.user_id
```

Если user отсутствует, Tokenio создаёт user record.

Если user существует, используется existing record.

Если user disabled:

```text
provisioning запрещён
```

Billing JWT не используется для идентификации provisioning caller и не хранится в provisioning record.

---

# 6. Internal endpoint: provision

Endpoint:

```http
POST /internal/v1/api-key-provisionings
X-Service-Token: <TOKENIO_PROVISIONING_SERVICE_TOKEN>
Idempotency-Key: <stable_provisioning_request_id>
Content-Type: application/json
```

`Idempotency-Key` обязателен.

Request:

```json
{
  "external_billing_user_id": "billing_usr_...",
  "source_reference": "payment_or_order_reference",
  "key_name": "Telegram payment key"
}
```

Поля:

```text
external_billing_user_id — обязательный stable billing subject
source_reference         — обязательная opaque ссылка на подтверждённую оплату/заказ
key_name                 — optional display name
```

`source_reference` не является доказательством оплаты для Tokenio. Доверие основано на service credential caller-а.

---

# 7. Provision result

Response shape:

```json
{
  "data": {
    "result": "created",
    "provisioning_id": "prov_...",
    "provisioning_status": "pending_delivery",
    "api_key_id": "ak_...",
    "api_key": "sk_live_...",
    "key_prefix": "sk_live_abcd...",
    "expires_at": "..."
  }
}
```

Allowed `result` values:

```text
created
replayed
already_provisioned
```

`api_key` присутствует только если:

```text
result IN (created, replayed)
AND provisioning_status = pending_delivery
AND encrypted delivery state is valid
```

При:

```text
result = already_provisioned
```

response не содержит raw API key.

---

# 8. First provisioning flow

В одной database transaction Tokenio должен:

```text
1. validate service credential
2. validate Idempotency-Key
3. lock idempotency scope
4. find or create user by external_billing_user_id
5. reject disabled user
6. check existing active API keys
7. generate cryptographically secure raw sk_...
8. calculate HMAC-SHA256 key_hash
9. create tokenio_api_keys record
10. encrypt raw key with provisioning encryption key
11. create tokenio_api_key_provisionings record
12. commit transaction
13. decrypt only for response serialization
14. return raw key
```

Database commit должен произойти до отправки response.

Если commit failed, raw key не считается provisioned и не должен возвращаться как successful result.

---

# 9. Idempotency and replay

Idempotency scope первой версии:

```text
Idempotency-Key
```

`Idempotency-Key` должен быть globally unique для trusted provisioning callers.

Повторный request с тем же key и тем же normalized input:

```text
1. не создаёт нового user
2. не создаёт нового API key
3. не создаёт нового provisioning record
4. если status=pending_delivery, возвращает тот же raw sk_...
5. возвращает result=replayed
```

Повторный request с тем же key, но другим:

```text
external_billing_user_id
source_reference
```

возвращает:

```text
HTTP 409
error.code = provisioning_conflict
```

Если same-idempotency provisioning уже:

```text
delivered
```

response не содержит raw key.

Если same-idempotency provisioning уже:

```text
expired
```

ответ:

```text
HTTP 410
error.code = provisioning_expired
```

Idempotency record не должен быть переиспользован для генерации нового key после expiration.

---

# 10. Existing key and subsequent top-ups

Новый API key не создаётся при каждом пополнении.

Перед созданием key Tokenio проверяет, существует ли у user активный API key:

```text
enabled = true
revoked_at IS NULL
expires_at IS NULL OR expires_at > now()
```

Если активный key существует и нет replay-able `pending_delivery` record:

```text
result = already_provisioned
api_key is absent
key_prefix may be returned
```

Это означает:

```text
баланс пополнен
существующий Tokenio API key продолжает работать
новый key автоматически не создаётся
```

Потеря уже доставленного key решается отдельной операцией rotation/revoke-and-create, а не повторным top-up provisioning.

---

# 11. Delivery confirmation

После успешной передачи raw key пользователю trusted caller вызывает:

```http
POST /internal/v1/api-key-provisionings/{provisioning_id}/confirm-delivery
X-Service-Token: <TOKENIO_PROVISIONING_SERVICE_TOKEN>
```

Операция idempotent.

Transition:

```text
pending_delivery -> delivered
```

В одной transaction Tokenio должен:

```text
1. verify provisioning status
2. set status = delivered
3. set delivered_at = now()
4. delete encrypted_raw_key
5. delete encryption_nonce
6. persist terminal state
```

Повторный confirm для `delivered` возвращает success без изменения state.

Delivery confirmation означает:

```text
trusted caller принял ответственность за доставку
```

Delivery confirmation не доказывает, что человек прочитал Telegram message или сохранил key.

Абсолютная exactly-once доставка человеку не гарантируется.

Гарантируется:

```text
same-key retry до confirm-delivery
no new key on retry
no raw-key recovery after confirm-delivery
```

---

# 12. Failure window

Если caller отправил key пользователю, но упал до `confirm-delivery`:

```text
повторный provision request возвращает тот же key
caller может повторно отправить тот же secret
новый API key не создаётся
```

Duplicate delivery одного и того же key допустима.

Duplicate creation разных keys для одного idempotency scope запрещена.

---

# 13. Expiration

Config:

```text
TOKENIO_API_KEY_PROVISIONING_TTL
```

Default:

```text
24h
```

Если delivery не подтверждена до `expires_at`, Tokenio должен transactionally:

```text
1. set provisioning status = expired
2. clear encrypted_raw_key
3. clear encryption_nonce
4. revoke associated tokenio_api_keys record
5. set expired_at
```

После expiration associated API key не должен проходить public authentication.

Expired raw key не восстанавливается.

Manual recovery выполняется через новый explicit admin/key-rotation flow, но не через reuse старого `Idempotency-Key`.

---

# 14. Encryption contract

Algorithm первой версии:

```text
AES-256-GCM
```

Encryption key config:

```text
TOKENIO_API_KEY_PROVISIONING_ENCRYPTION_KEY
```

Representation:

```text
base64-encoded 32-byte key
```

Key version config:

```text
TOKENIO_API_KEY_PROVISIONING_KEY_VERSION
```

Default:

```text
v1
```

Для каждого record используется cryptographically secure unique nonce.

Associated authenticated data должна включать:

```text
provisioning_id
api_key_id
user_id
```

Это запрещает безопасно переставлять ciphertext между records.

Encryption key:

```text
не хранится в Postgres
не логируется
не возвращается через API
не совпадает с TOKENIO_API_KEY_HASH_SECRET
```

Первая версия использует один active encryption key.

Rotation разрешена только если:

```text
нет pending_delivery records, encrypted active key которых требует старую version
```

Поддержка keyring/multiple decryption keys требует отдельного contract update.

---

# 15. Provisioning states

Allowed statuses:

```text
pending_delivery
delivered
expired
```

Allowed transitions:

```text
pending_delivery -> delivered
pending_delivery -> expired
```

Terminal statuses:

```text
delivered
expired
```

Forbidden transitions:

```text
delivered -> pending_delivery
expired -> pending_delivery
expired -> delivered
```

---

# 16. Storage invariant

`tokenio_api_keys` остаётся permanent authentication source of truth.

Он не содержит reversible raw key.

`tokenio_api_key_provisionings` является temporary delivery state.

Для `pending_delivery`:

```text
encrypted_raw_key IS NOT NULL
encryption_nonce IS NOT NULL
expires_at IS NOT NULL
```

Для `delivered` или `expired`:

```text
encrypted_raw_key IS NULL
encryption_nonce IS NULL
```

Один user не должен иметь больше одного `pending_delivery` provisioning одновременно.

---

# 17. Concurrency

Provisioning creation должна использовать transaction и database uniqueness.

Минимальные guards:

```text
unique Idempotency-Key
one pending_delivery provisioning per user
unique API key hash
user lookup/create serialized by external_billing_user_id
```

Concurrent requests с одним `Idempotency-Key` должны завершиться одним record и одним API key.

Application-level in-memory mutex не является source of truth.

---

# 18. Internal errors

Provisioning API использует gateway-owned envelope:

```json
{
  "error": {
    "code": "provisioning_conflict",
    "message": "Provisioning request conflicts with existing state",
    "request_id": "provreq_..."
  }
}
```

Stable codes:

```text
provisioning_unauthorized
provisioning_invalid_request
provisioning_conflict
provisioning_expired
provisioning_store_unavailable
provisioning_crypto_unavailable
```

Detailed status mapping находится в:

```text
docs/spec/080-error-model.ru.md
```

Error response не должен содержать:

```text
raw API key
encrypted_raw_key
encryption nonce
encryption key/version secret value
HMAC hash
service token
```

---

# 19. Logging

Разрешено логировать:

```text
provisioning_request_id
provisioning_id
idempotency key hash
external_billing_user_id
user_id
api_key_id
key_prefix
result
status
source_reference hash
expires_at
delivered_at
expired_at
error_code
```

Запрещено логировать:

```text
raw API key
encrypted_raw_key
encryption nonce
provisioning encryption key
provisioning service token
Authorization headers
X-Service-Token
```

Plain `Idempotency-Key` и `source_reference` по умолчанию не логируются; используется hash.

---

# 20. Admin visibility

Endpoint:

```http
GET /admin/v1/api-key-provisionings
Authorization: Bearer <TOKENIO_ADMIN_TOKEN>
```

Allowed filters:

```text
external_billing_user_id
user_id
api_key_id
status
result_type
created_from
created_to
limit
offset
```

Admin API может показывать:

```text
provisioning_id
external_billing_user_id
user_id
api_key_id
key_prefix
result_type
status
source_reference hash
created_at
expires_at
delivered_at
expired_at
```

Admin API никогда не возвращает:

```text
raw API key
encrypted_raw_key
encryption nonce
provisioning encryption key
```

---

# 21. Tests

Обязательные tests:

```text
first request creates one key and pending delivery record
same idempotency request returns same raw key
same idempotency with different input returns conflict
concurrent same-idempotency requests create one key
existing active key returns already_provisioned
subsequent top-up does not create another key
confirm delivery clears ciphertext and nonce
repeated confirm is idempotent
delivered provisioning never returns raw key
expiration clears ciphertext and revokes API key
expired provisioning returns provisioning_expired
disabled user cannot be provisioned
invalid service token is rejected
raw key is absent from logs/errors/audit
encryption uses unique nonce and authenticated associated data
startup rejects invalid encryption key
```

---

# 22. Acceptance criteria

Provisioning реализован корректно, если:

```text
1. Provisioning API не является public LLM API.
2. Provisioning использует отдельный service credential.
3. Caller передаёт stable external_billing_user_id.
4. Idempotency-Key обязателен.
5. First request создаёт user/key/provisioning transactionally.
6. Permanent API key storage содержит только HMAC hash.
7. Temporary reversible copy хранится только encrypted.
8. Same-idempotency retry возвращает тот же raw key до delivery confirmation.
9. Retry не создаёт второй API key.
10. Subsequent top-up не создаёт новый key при наличии active key.
11. Confirm-delivery удаляет encrypted copy.
12. После confirm-delivery raw key невозможно получить через API.
13. Expiration удаляет encrypted copy и revoke-ит undelivered key.
14. Raw key не логируется.
15. Admin API не раскрывает encrypted delivery state.
16. Concurrency защищена database constraints/transactions.
17. Tests покрывают response-loss и confirm-loss windows.
```
