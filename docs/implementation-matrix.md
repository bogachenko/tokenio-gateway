# Tokenio Gateway implementation matrix

Статус документа: active  
Source of truth: `docs/spec/*`, `docs/adr/*`, executable tests and the current GitHub revision.

## Status values

| Status | Meaning |
|---|---|
| `specified` | Нормативный contract однозначно закреплён в spec/ADR. |
| `implemented` | Production implementation существует. |
| `verified` | Implementation подтверждён automated acceptance evidence. |
| `unsupported` | Возможность явно исключена текущей спецификацией. |
| `pending` | Требование утверждено, но executable evidence ещё не закрыт. |

Наличие interface, struct, repository method или constructor само по себе не является
evidence реализации.

## Baseline decisions

| Requirement ID | Spec / section | Requirement | Implementation | Automated test | Status | Evidence |
|---|---|---|---|---|---|---|
| `BASE-CATALOG-001` | `010 §7.4.1`, `030 §17` | Public model capabilities — conservative intersection доступных routes. | Existing model-catalog implementation requires separate audit. | Pending | `specified` | `docs/spec/010-external-api.ru.md`, `docs/spec/030-routing-and-resellers.ru.md` |
| `BASE-AUTH-001` | `011 §2.1`, `020 §3.1` | Native SDK auth carriers нормализуются transport adapter-ом в единый Tokenio raw API key. | Runtime implementation belongs to native-family roadmap stages. | Pending | `specified` | `docs/adr/0002-native-api-auth-carriers.md` |
| `BASE-RECOVERY-001` | `050 §10.3A`, `090 §7.4-7.5` | Billing recovery запускается сразу при старте и затем периодически, независимо от новых LLM requests. | Worker implementation belongs to billing-recovery roadmap stage. | Pending | `specified` | `docs/spec/050-ledger-and-auto-charge.ru.md`, `docs/spec/090-configuration.ru.md` |
| `BASE-MIGRATION-001` | `090 §17.3` | `cmd/migrate` применяет migrations; `cmd/gateway` только проверяет schema compatibility. | Runtime implementation belongs to storage/migrations roadmap stage. | Pending | `specified` | `docs/adr/0003-migration-execution-policy.md` |

## Roadmap verification groups

| Requirement group | Applicable specs | Status |
|---|---|---|
| Application dependency direction | `AGENTS.md`, ADR layering | `verified`: domain financial contracts; compatibility wrappers; repository-wide architecture test |
| OpenAI-compatible public contract | `010`, `020`, `030`, `040`, `050`, `080`, `090` | `verified`: byte-preserving request/response boundaries, model-only rewrite, structural JSON limits, pricing_failed passthrough, complete billing headers, minimum balance, API-key last_used_at and all idempotency states have automated evidence |
| Storage and migration contract | `070`, `090`, migration ADR | `verified`: complete 16-table and 23-FK schema manifest, canonical CHECK/UNIQUE/index contracts, migration lifecycle, exact usage dimensions, immutable ordered charge command, exact float64 markup, SQL-enforced usage CAS, lifecycle UTC timestamps, durable billing, operational-history and API-key-provisioning parent-delete protection, and forwarding-attempt ownership cascade |
| Durable billing recovery | `050`, `090` | `verified`: bounded persisted-command discovery, application recovery cycle, immediate periodic worker, runtime wiring, and restart-safe pending/failed Postgres integration evidence |
| Operational routing policy | `030`, `080`, `090` | `pending` |
| Telegram alert vertical slice | `030`, `060`, `070`, `090` | `pending` |
| Admin and provisioning acceptance | `021`, `060`, `070`, `080`, `090` | `pending` |
| Native API families | `011`, `020`, `030`, `040`, `080`, native-auth ADR | `pending` |
| Reproducible integration environment | all applicable specs | `pending` |
| Production verification | all applicable specs and ADRs | `pending` |

Every implementation PR must update the corresponding row with exact code paths,
test paths and executable verification evidence.
