# 050. Ledger and Auto-Charge

Версия: 0.1
Статус: draft
Язык: русский
Проект: `github.com/bogachenko/tokenio-gateway`

---

# 1. Назначение документа

Этот документ описывает local ledger и auto-charge механизм Tokenio Gateway.

Документ фиксирует:

```text
usage lifecycle
local request reserve
billable usage
pending amount
effective user balance
auto-charge threshold
billing charge batching
partial charge
idempotency
billing failure behavior
reseller cost reconciliation boundary
```

Документ не описывает:

```text
public HTTP API
API key validation
route selection
usage extraction formula
pricing formula
admin API endpoint details
database migrations
```

Эти темы описываются в отдельных спецификациях.

---

# 2. Главный ledger invariant

Tokenio Gateway должен иметь локальный ledger как source of truth для всех LLM usage events.

Каждый LLM request должен быть представлен в ledger через стабильный:

```text
local_request_id
```

Ledger нужен для:

```text
preflight reserve
idempotency
pending balance calculation
billing charge batching
retries after billing failure
admin/debug visibility
auditability
```

Запрещено:

```text
списывать деньги без локальной usage записи
терять successful upstream usage из-за billing failure
делать публичный /billing/flush
считать user balance только по remote billing balance без pending ledger
создавать второй billable usage для того же idempotency scope
```

---

# 3. Ledger states

## 3.1. Usage statuses

Минимальные статусы usage record:

```text
reserved
released
billable
partially_charged
charged
failed
pricing_failed
```

## 3.2. reserved

`reserved` означает:

```text
request принят gateway
user/auth/routing/preflight прошли
local usage reserve создан
upstream request ещё не завершён
```

Reserved record содержит:

```text
local_request_id
idempotency_key
user_id
api_key_id
api_family
endpoint_kind
client_model
billing_model
selected_route_id
selected_reseller_id
provider_type
provider_model
estimated_usage
estimated_client_amount_cents
estimated_upstream_cost_cents
currency
status = reserved
created_at
```

## 3.3. released

`released` означает:

```text
request не стал billable
upstream не был safely processed
reserve освобождён
```

Примеры:

```text
route selection failed after reserve
connection failed before upstream accepted request
timeout before response headers
client request rejected before upstream
```

Released record не участвует в pending billing.

## 3.4. billable

`billable` означает:

```text
upstream request successful
usage рассчитан
client amount рассчитан
usage ожидает списания через billing service
```

Billable record участвует в:

```text
pending amount
effective user balance
auto-charge batch selection
```

## 3.5. partially_charged

`partially_charged` означает:

```text
record был списан частично
remaining_amount_cents > 0
```

Partial charge допустим, если billing balance меньше total pending amount, но больше нуля.

## 3.6. charged

`charged` означает:

```text
usage полностью списан через billing service
remaining_amount_cents = 0
```

Charged record больше не участвует в pending amount.

## 3.7. failed

`failed` означает:

```text
request завершился ошибкой и не должен быть списан
```

Failed record нужен для audit/debug, но не участвует в pending billing.

## 3.8. pricing_failed

`pricing_failed` означает:

```text
upstream request successful
response клиенту мог быть возвращён
usage/pricing не удалось безопасно рассчитать
```

`pricing_failed` не должен автоматически списываться.

Он должен быть виден через admin API для manual resolution.

До resolution gateway должен учитывать такой record как unresolved billing risk согласно policy.

Policy первой версии:

```text
pricing_failed блокирует новые LLM requests пользователя,
если admin не пометил record как resolved/free/charged/failed.
```

---

# 4. State transitions

## 4.1. Allowed transitions

Разрешённые переходы:

```text
reserved -> released
reserved -> billable
reserved -> failed
reserved -> pricing_failed

billable -> charged
billable -> partially_charged
billable -> failed

partially_charged -> charged
partially_charged -> partially_charged
partially_charged -> failed

pricing_failed -> billable
pricing_failed -> charged
pricing_failed -> failed

released -> terminal
charged -> terminal
failed -> terminal
```

## 4.2. Forbidden transitions

Запрещено:

```text
charged -> billable
charged -> reserved
released -> billable
failed -> charged без admin/manual resolution
создать новый billable record вместо обновления existing idempotent record
```

## 4.3. Terminal states

Terminal states:

```text
released
charged
failed
```

---

# 5. Local request reserve

## 5.1. When reserve is created

Reserve создаётся после:

```text
auth success
request body parsed enough for routing
client_model extracted
route selected
preflight estimate calculated
user effective balance check passed
reseller reserve created
```

Reserve создаётся до upstream request.

## 5.2. Reserve amount

Reserve amount для user ledger:

```text
estimated_client_amount_cents
```

Reserve amount для reseller accounting:

```text
estimated_upstream_cost_cents
```

Эти значения нельзя смешивать.

## 5.3. Reserve purpose

User reserve нужен для:

```text
не принять слишком много параллельных запросов
учесть pending в effective balance
поддержать idempotency
```

Reseller reserve нужен для:

```text
не превысить расчётный баланс reseller
```

## 5.4. Reserve release

Если request safely failed before upstream processing:

```text
usage.status = released
released_at = now
failure_reason = <reason>
```

User pending reserve освобождается.

Reseller reserve освобождается в routing/reseller accounting layer.

---

# 6. User balance

## 6.1. Remote balance

Remote balance — баланс пользователя в billing service:

```text
remote_balance_cents
```

Он получается через:

```http
GET /api/v1/wallet/balance
Authorization: Bearer <billing_jwt>
```

## 6.2. Local pending amount

Local pending amount считается из ledger:

```text
pending_amount_cents =
  sum(reserved.estimated_client_amount_cents)
+ sum(billable.remaining_amount_cents OR billable.client_amount_cents)
+ sum(partially_charged.remaining_amount_cents)
```

`charged`, `released`, `failed` не участвуют в pending.

`pricing_failed` учитывается отдельно как unresolved risk.

## 6.3. Effective balance

Effective balance:

```text
effective_balance_cents =
  remote_balance_cents - pending_amount_cents
```

Preflight request можно принять только если:

```text
effective_balance_cents >= estimated_client_amount_cents
AND effective_balance_cents >= TOKENIO_MIN_REQUEST_BALANCE_CENTS
```

## 6.4. Cached billing session

Gateway может хранить local billing session:

```text
user_id
remote_balance_cents
currency
fetched_at
updated_at
```

Cached session не заменяет ledger.

Cached session используется вместе с pending amount.

## 6.5. Balance refresh

Если cached effective balance недостаточен, gateway должен попытаться refresh remote balance через billing service.

Если после refresh effective balance всё равно недостаточен:

```text
HTTP 402
error.code = insufficient_funds
```

Если billing service недоступен, но cached balance есть, gateway может принять решение по cached effective balance.

Если cached balance отсутствует и billing service недоступен:

```text
HTTP 502
error.code = billing_unavailable
```

---

# 7. Preflight request flow

Перед upstream request gateway должен выполнить:

```text
1. Validate user API key.
2. Extract api_family, endpoint_kind, client_model.
3. Detect requested capabilities.
4. Estimate usage.
5. Select route.
6. Estimate client amount.
7. Estimate upstream cost.
8. Load cached user billing session.
9. Calculate pending amount.
10. Refresh remote balance if needed.
11. Check effective balance.
12. Create reseller reserve.
13. Create local usage reserve.
14. Forward request upstream.
```

Если любой шаг до upstream request падает, billable usage не создаётся.

---

# 8. Successful upstream flow

После successful upstream response gateway должен:

```text
1. Extract actual usage or estimate conservatively.
2. Calculate actual upstream cost.
3. Calculate actual client amount.
4. Reconcile reseller reserve.
5. Update usage record:
   reserved -> billable
6. Add billing headers.
7. Try auto-charge if threshold reached.
8. Return original upstream response body.
```

Если auto-charge failed, response body всё равно возвращается клиенту.

Usage остаётся pending.

---

# 9. Billing failure after successful upstream

## 9.1. Main rule

Если LLM upstream request успешен, а billing service недоступен при auto-charge:

```text
response клиенту возвращается
usage остаётся billable или partially_charged
future requests учитывают pending
```

Gateway не должен терять usage.

Gateway не должен повторно отправлять upstream request только из-за billing failure.

## 9.2. Future requests after billing failure

Будущие request пользователя разрешены только если:

```text
remote_or_cached_balance_cents - pending_amount_cents
>= estimated_client_amount_cents
```

Если pending съел effective balance:

```text
HTTP 402
error.code = insufficient_funds
```

Если нужно refresh balance, а billing service недоступен:

```text
HTTP 502
error.code = billing_unavailable
```

---

# 10. Auto-charge

## 10.1. Public flush forbidden

Публичный endpoint `/billing/flush` запрещён.

Списание выполняется внутри gateway автоматически.

## 10.2. Auto-charge trigger

Auto-charge запускается после successful billable commit, если:

```text
pending_billable_amount_cents >= TOKENIO_AUTO_CHARGE_THRESHOLD_CENTS
```

Config:

```text
TOKENIO_AUTO_CHARGE_THRESHOLD_CENTS
```

Threshold берётся из environment.

## 10.3. Minimum charge amount

Если pending amount меньше:

```text
TOKENIO_MIN_CHARGE_AMOUNT_CENTS
```

gateway может отложить charge.

Default первой версии:

```text
TOKENIO_MIN_CHARGE_AMOUNT_CENTS=100
```

## 10.4. Charge candidates

Auto-charge выбирает records пользователя со статусами:

```text
billable
partially_charged
```

Порядок:

```text
created_at ASC
local_request_id ASC
```

## 10.5. Charge batch

Gateway формирует stable billing charge request id:

```text
billing_charge_request_id
```

Он deterministic для canonical financial command и используется как:

```http
Idempotency-Key: <billing_charge_request_id>
```

`StableBatchID` идентифицирует immutable financial command. Runtime-generated timestamps и mutable batch result state не являются частью stable ID.

## 10.5A. Charge batch preparation and usage claim

До внешнего Billing-вызова gateway обязан transactionally подготовить durable charge command.

`ports.UsageChargeBatchPlan.ExpectedRecords` содержит exact `pre-claim` состояния usage records.

`UsageLedger.PrepareChargeBatch` в одной transaction обязан:

```text
1. Lock allocated usage records в canonical order.
2. Сравнить каждую строку с соответствующим pre-claim ExpectedRecord.
3. Проверить отсутствие другого active pending/failed batch claim.
4. Создать pending billing batch.
5. Сохранить ordered allocations.
6. Установить для каждого allocated usage record:
   billing_charge_request_id = batch.id.
7. Создать immutable ordered post-claim expected-record snapshots.
8. Commit.
```

Post-claim expected record равен pre-claim record, кроме:

```text
BillingChargeRequestID = batch.ID
```

Claim не изменяет:

```text
usage status
charged_amount_cents
remaining_amount_cents
usage timestamps
```

Claim не является ledger status transition.

`ports.BillingChargeBatchSnapshot.ExpectedRecords` всегда содержит post-claim records.

Allocation с `charged_amount_cents <= 0` является invalid charge plan и не сохраняется.

Billing Service вызывается только после successful commit `PrepareChargeBatch`.

## 10.5B. Existing batch replay

Если batch с `plan.Batch.ID` уже существует, adapter не сравнивает incoming pre-claim records напрямую с persisted post-claim snapshots.

Для каждого incoming record строится canonical comparison form:

```text
comparison_record[i] =
    plan.ExpectedRecords[i]
    with BillingChargeRequestID = plan.Batch.ID
```

Transformation меняет только `BillingChargeRequestID`.

### Canonical batch replay identity

Exact immutable batch comparison включает только:

```text
ID
UserID
BillingSubjectUserID
ProviderType
ClientModel
BillingModel
InputTokens
OutputTokens
AmountCents
Currency
```

Каждый incoming `plan.Batch` до create/replay branching обязан иметь:

```text
Status = pending
CreatedAt = UpdatedAt
CreatedAt и UpdatedAt являются valid UTC timestamps
```

Следующие поля не входят в existing-command equality:

```text
Status
BillingResponseBalanceCents
BillingErrorCode
ChargedAt
FailedAt
CreatedAt
UpdatedAt
```

Причины:

```text
Status и result fields являются mutable lifecycle state.
CreatedAt и UpdatedAt создаются текущим clock,
не входят в StableBatchID и отличаются у legitimate replay,
построенного в другое время.
```

Persisted batch timestamps первой successful preparation являются authoritative и возвращаются replay caller без изменения.

### Canonical allocation replay identity

Allocations сравниваются по persisted `position` и полям:

```text
ID
BatchID
LocalRequestID
ChargedAmountCents
RemainingAmountCents
```

Каждая incoming allocation до create/replay branching обязана иметь:

```text
CreatedAt = plan.Batch.CreatedAt
CreatedAt является valid UTC timestamp
```

`BillingChargeAllocation.CreatedAt`:

```text
валидируется при initial create
не входит в StableBatchID
не входит в existing-command equality
не заменяет persisted CreatedAt при replay
```

Incoming slice index задаёт canonical position. Persisted allocation timestamps первой successful preparation являются authoritative.

### Canonical expected-record replay identity

`comparison_record[i]` exact-сравнивается с persisted post-claim expected record на той же position по всем canonical `UsageRecord` fields, включая usage timestamps.

Exact replay требует совпадения:

```text
canonical immutable batch identity
canonical ordered allocation identity
comparison records с persisted post-claim snapshots
record/allocation local_request_id correspondence по position
```

Exact match:

```text
idempotent success
return exact persisted BillingChargeBatchSnapshot
```

Если exact persisted snapshot имеет:

```text
Status = succeeded
```

это legitimate concurrent replay уже reconciled financial command, а не store corruption.

`PrepareChargeBatch` возвращает exact persisted succeeded snapshot. Application caller обязан:

```text
не вызывать BillingChargeClient.Charge повторно
не вызывать MarkChargeBatchFailed
не вызывать ApplyChargeSuccess
вернуть idempotent processed result из persisted batch
использовать persisted BillingResponseBalanceCents, если он присутствует
```

`AutoChargeService.processPreparedBatch` принимает `pending`, `failed` и `succeeded` snapshots. Для `succeeded` он завершает operation до любого внешнего Billing side effect или ledger mutation.

Если persisted `succeeded` snapshot не содержит:

```text
BillingResponseBalanceCents
```

application не имеет права вычитать `batch.AmountCents` из ранее загруженного remote balance. Ранее загруженный balance мог быть получен как до, так и после concurrent successful charge, поэтому локальное вычитание неоднозначно и может повторно применить уже совершённый financial effect.

Перед подготовкой следующей provider/model group application обязана:

```text
1. Повторно вызвать BillingBalanceClient.GetBalance.
2. Валидировать currency и non-negative balance.
3. Использовать refreshed balance как remaining remote balance.
```

Если refresh недоступен или возвращает invalid balance:

```text
ErrBillingUnavailable
```

Новые Billing charge calls и новые ledger mutations после такого replay не выполняются до successful refresh.

После последней provider/model group refresh не выполняется. Если следующей group нет, `succeeded` replay завершается успешно даже при недоступном Billing balance endpoint: пересчитанный `remainingRemote` больше не используется для нового financial decision.

Для `pending`/`failed` snapshot, который был реально списан текущим вызовом и получил successful Billing response без balance, application вычитает `batch.AmountCents` ровно один раз из balance, загруженного до этого charge, только если значение требуется для решения по следующей group.

Любое отличие:

```text
state/contract conflict
```

При existing batch replay usage claims повторно не обновляются, child rows повторно не вставляются и persisted timestamps не изменяются.

## 10.5C. Active and historical claims

Для usage record:

```text
billing_charge_request_id referencing pending batch
    -> active claim

billing_charge_request_id referencing failed batch
    -> active claim retained for exact retry

billing_charge_request_id referencing succeeded batch
    -> historical charge reference
```

Record с active claim нельзя включить в другой batch.

`partially_charged` record с historical succeeded batch reference может участвовать в следующем charge.

При подготовке следующего batch его historical ID заменяется ID нового pending batch, а immutable expected snapshot сохраняет новое post-claim состояние.

## 10.6. Charge amount

Charge amount:

```text
charge_amount_cents = min(pending_chargeable_amount_cents, remote_balance_cents)
```

Если `charge_amount_cents <= 0`:

```text
batch не создаётся
auto-charge deferred
records остаются billable/partially_charged
```

## 10.7. Partial charge

Если billing balance меньше pending amount, но больше нуля:

```text
часть records помечается charged
последний record может стать partially_charged
remaining_amount_cents сохраняется
```

## 10.8. Successful charge

После successful Billing response gateway transactionally:

```text
1. Загружает persisted immutable batch command.
2. Проверяет caller metadata против persisted command.
3. Проверяет current usage rows против persisted post-claim expected snapshots.
4. Применяет allocations ровно один раз.
5. Обновляет charged_amount_cents и remaining_amount_cents.
6. Устанавливает status charged или partially_charged.
7. Устанавливает charged_at.
8. Сохраняет billing_charge_request_id текущего batch.
9. Переводит batch в succeeded.
10. Сохраняет Billing response balance, если он присутствует.
```

`billing_charge_request_id` уже установлен на preparation stage; successful reconciliation сохраняет его как historical succeeded batch reference.

Identical succeeded replay является no-op: usage rows, immutable command и batch timestamps не изменяются.

## 10.9. Failed charge

Если Billing charge failed:

```text
batch pending -> failed
usage records остаются billable/partially_charged
charged и remaining amounts не изменяются
active billing_charge_request_id claim сохраняется
future retry использует тот же batch ID и тот же persisted command
```

Новый batch для этих records не создаётся, пока failed batch не reconciled либо не разрешён отдельной explicit recovery operation.

Gateway не удаляет pending usage и не сохраняет raw Billing response body.

## 10.10. MarkChargeBatchFailed CAS

Этот контракт относится к:

```text
UsageLedger.MarkChargeBatchFailed
```

и описывает initial automatic charge failure либо idempotent replay той же automatic failure operation.

Первый переход:

```text
persisted status = pending
expectedStatus = pending
-> pending -> failed
```

Сохраняются:

```text
billing_error_code
failed_at
updated_at = failed_at
```

Если persisted status уже `failed`, idempotent replay разрешён только когда:

```text
expectedStatus = failed
billingErrorCode точно совпадает с persisted billing_error_code
```

Caller `failedAt` при already-failed replay:

```text
не участвует в equality
не сохраняется
не изменяет failed_at
не изменяет updated_at
```

Другой error code или другой expected status возвращает:

```text
state conflict
```

`succeeded -> failed` запрещён.

`MarkChargeBatchFailed` не создаёт admin retry outcome audit и не моделирует новую явную попытку retry.

## 10.11. Explicit failed-batch retry and audit

Явный admin retry failed batch является новой operation, а не idempotent replay `MarkChargeBatchFailed`.

До внешнего Billing-вызова:

```text
AdminUsageLedger.RecordChargeRetryAttemptWithAudit
```

атомарно:

```text
1. Проверяет, что exact persisted failed snapshot всё ещё current.
2. Не изменяет BillingChargeBatch.
3. Сохраняет attempt audit:
   before_state = exact current batch
   after_state = exact current batch.
4. Commit до внешнего financial side effect.
```

Если явный retry снова завершился Billing error, вызывается:

```text
AdminUsageLedger.MarkChargeRetryFailedWithAudit
```

с:

```text
expectedStatus = failed
billingErrorCode = normalized retry error code
retryFailedAt = valid UTC operation time
outcome audit before_state = exact pre-operation batch
outcome audit after_state = exact requested post-operation batch
```

Одна transaction обязана:

```text
1. Lock current batch.
2. Проверить current status = expectedStatus = failed.
3. Построить committed next batch:
   Status = failed
   BillingErrorCode = billingErrorCode
   FailedAt = retryFailedAt
   UpdatedAt = retryFailedAt.
4. Не изменять immutable command, ChargedAt или BillingResponseBalanceCents.
5. Exact-сравнить audit before_state с current batch.
6. Exact-сравнить audit after_state с next batch.
7. Сохранить next batch и outcome audit атомарно.
8. Commit.
```

Это допустимый audited transition:

```text
failed -> failed
```

который фиксирует outcome новой внешней retry attempt.

Инвариант audit:

```text
persisted audit before_state = exact pre-transaction entity
persisted audit after_state = exact committed entity
returned failed entity = exact committed entity
```

Idempotency определяется outcome audit ID:

```text
same outcome audit ID + exact same operation payload
    -> idempotent no-op returning the already committed state

same outcome audit ID + different payload
    -> state/contract conflict
```

Новая retry attempt имеет новые deterministic phase audit IDs и может обновить `failed_at`/`updated_at`.

Если current batch уже `succeeded`, retry failure outcome возвращает state conflict и не создаёт ложный audit.

---

# 11. Billing charge request

Gateway вызывает:

```http
POST /api/v1/usage/charge
X-Service-Token: <TOKENIO_BILLING_SERVICE_TOKEN>
Idempotency-Key: <billing_charge_request_id>
Content-Type: application/json
```

Body:

```json
{
  "request_id": "<billing_charge_request_id>",
  "user_id": "<billing_subject_user_id>",
  "model": "openrouter:gpt-4.1-mini",
  "input_tokens": 1000,
  "output_tokens": 500,
  "amount_cents": 123,
  "currency": "RUB"
}
```

Если batch содержит несколько provider/model groups, gateway должен группировать charge по:

```text
provider_type + client_model
```

Billing model string:

```text
provider_type:client_model
```

Для каждого group формируется отдельный charge request.

---

# 12. Grouping

## 12.1. Charge grouping key

Charge grouping key:

```text
user_id + provider_type + client_model + currency
```

Billing service получает `model`:

```text
provider_type:client_model
```

## 12.2. Why grouping is required

Grouping нужен, потому что billing service contract содержит одно поле:

```text
model
```

Если смешать разные models в один charge, billing service потеряет model-level accounting.

## 12.3. Mixed pending records

Если pending records содержат несколько groups, gateway должен:

```text
1. split records by group
2. charge each group separately
3. preserve idempotency per group
4. update records independently
```

---

# 13. Idempotency

## 13.1. Client idempotency

Если client передал:

```http
Idempotency-Key: <value>
```

scope:

```text
user_id + endpoint_kind + idempotency_key
```

## 13.2. Existing reserved request

Если повторный запрос пришёл с тем же idempotency scope и existing record в статусе `reserved`:

```text
HTTP 409
error.code = request_in_progress
```

## 13.3. Existing billable/charged request

Если existing record уже `billable`, `partially_charged` или `charged`, gateway не должен создавать второй billable usage.

Response replay policy первой версии:

```text
HTTP 409
error.code = idempotency_replay_not_available
```

Причина:

```text
gateway не хранит full upstream response body в ledger первой версии
```

Будущая версия может хранить response body hash/body для replay.

## 13.4. Existing failed/released request

Если existing record `failed` или `released`, повторный запрос с тем же idempotency key может быть разрешён только если это явно описано policy.

Policy первой версии:

```text
возвращать HTTP 409 idempotency_key_reused
```

---

# 14. Pricing failed state

## 14.1. Upstream success but pricing failed

Если upstream successful, но usage/pricing failed:

```text
usage.status = pricing_failed
response body клиенту возвращается
X-Billing-Status: pricing_failed
auto-charge не запускается
```

## 14.2. Blocking policy

Пока у пользователя есть unresolved `pricing_failed` records:

```text
новые LLM requests блокируются
```

Ответ:

```text
HTTP 409
error.code = unresolved_usage
```

Admin должен вручную resolve record.

## 14.3. Manual resolution

Admin может перевести `pricing_failed` в:

```text
billable
charged
failed
```

Manual resolution должен быть audit logged.

---

# 15. Response headers

После successful billable commit gateway добавляет:

```http
X-Local-Request-ID: llmreq_...
X-Billing-Provider-Type: openrouter
X-Billing-Client-Model: gpt-4.1-mini
X-Billing-Model: openrouter:gpt-4.1-mini
X-Billing-Amount-Cents: 123
X-Billing-Currency: RUB
X-Wallet-Balance-Cents: 10000
X-Wallet-Effective-Balance-Cents: 9877
X-Billing-Pending-Cents: 123
```

Если auto-charge success happened before response:

```text
X-Billing-Pending-Cents может быть 0 или reduced
X-Wallet-Balance-Cents отражает актуальный balance после charge, если billing response его вернул
```

Если auto-charge failed:

```http
X-Billing-Auto-Charge-Status: failed
```

Если auto-charge deferred:

```http
X-Billing-Auto-Charge-Status: deferred
```

---

# 16. Ledger records

Минимальная canonical usage record model:

```text
local_request_id
idempotency_key
user_id
api_key_id
api_family
endpoint_kind
client_model
billing_model
selected_reseller_id
selected_route_id
provider_type
provider_model
provider_request_id
provider_response_model

estimated_input_tokens
estimated_cached_input_tokens
estimated_output_tokens
estimated_reasoning_tokens
estimated_image_input_tokens
estimated_audio_input_tokens
estimated_audio_output_tokens
estimated_file_input_tokens
estimated_video_input_tokens
estimated_image_generation_units
estimated_client_amount_cents
estimated_upstream_cost_cents

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
usage_completeness

client_amount_cents
charged_amount_cents
remaining_amount_cents
actual_upstream_cost_cents
currency

status
failure_reason
billing_charge_request_id

created_at
reserved_at
released_at
billable_at
charged_at
failed_at
updated_at
```

Все десять dimensions `EstimatedUsage` и `Usage` являются частью exact persistence round-trip.

---

# 17. Billing session records

Минимальная billing session model:

```text
user_id
billing_subject_user_id
remote_balance_cents
pending_amount_cents_cached
currency
fetched_at
created_at
updated_at
```

`pending_amount_cents_cached` может быть denormalized optimization.

Source of truth для pending всё равно ledger.

---

# 18. Consistency

## 18.1. Transaction boundaries

Операции ledger должны быть transactional.

Минимально transaction required для:

```text
create usage reserve
commit reserved to billable
mark charged allocations
mark pricing_failed
manual resolution
```

## 18.2. Idempotent writes

Writes должны быть idempotent по:

```text
local_request_id
idempotency scope
billing_charge_request_id
```

## 18.3. Clock

Все timestamps хранятся в UTC.

---

# 19. One instance assumption

Первая версия работает как:

```text
single instance
```

Но ledger хранится в Postgres.

Даже при single instance нельзя хранить usage только in-memory.

---

# 20. Admin visibility

Admin API должен позволять смотреть:

```text
usage records
pending amount by user
pricing_failed records
billable records
partially_charged records
billing charge batches
failed auto-charge attempts
```

Admin API должен позволять manual resolution для:

```text
pricing_failed
stuck reserved
failed charge diagnostics
```

Детальный admin contract описан в:

```text
docs/spec/060-admin-api.ru.md
```

---

# 21. Error mapping

Ledger-related errors:

```text
insufficient_funds
billing_unavailable
request_in_progress
idempotency_replay_not_available
idempotency_key_reused
unresolved_usage
usage_store_error
auto_charge_failed
```

Подробный response mapping описан в:

```text
docs/spec/080-error-model.ru.md
```

---

# 22. Logging

Разрешено логировать:

```text
local_request_id
user_id
api_key_id
endpoint_kind
client_model
billing_model
status
client_amount_cents
pending_amount_cents
billing_charge_request_id
auto_charge_status
error_code
```

Запрещено логировать:

```text
raw user API key
billing JWT
billing service token
reseller API key
Authorization header
full request body by default
```

---

# 23. Tests

Ledger layer должен иметь tests для:

```text
create reserved usage
reserved -> released
reserved -> billable
billable -> charged
billable -> partially_charged
partially_charged -> charged
pricing_failed blocks future requests
pending amount calculation
effective balance calculation
billing unavailable after successful upstream
auto-charge threshold trigger
auto-charge deferred below threshold
partial charge allocation
grouping by provider_type + client_model
stable billing_charge_request_id
client Idempotency-Key duplicate reserved
client Idempotency-Key duplicate charged
```

---

# 24. Acceptance criteria

Ledger and auto-charge layer считается реализованным, если:

```text
1. Каждый LLM request получает local_request_id.
2. До upstream создаётся reserved usage.
3. Safe failure переводит reserved -> released.
4. Successful upstream переводит reserved -> billable.
5. Pending amount считается из reserved/billable/partially_charged.
6. Effective balance = remote balance - pending.
7. Future requests учитывают pending.
8. Public /billing/flush отсутствует.
9. Auto-charge запускается по TOKENIO_AUTO_CHARGE_THRESHOLD_CENTS.
10. Billing charge использует X-Service-Token.
11. Billing charge использует stable Idempotency-Key.
12. Charge группируется по provider_type + client_model.
13. Billing failure после successful upstream не ломает response клиенту.
14. Billing failure оставляет usage pending.
15. pricing_failed не теряется и блокирует будущие requests до resolution.
16. Client Idempotency-Key не создаёт двойное списание.
17. Partial charge корректно обновляет remaining_amount_cents.
18. Ledger writes выполняются transactional.
19. Tests покрывают state transitions, pending/effective balance, auto-charge и idempotency.
```
