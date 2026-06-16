# AGENTS.md

## Source of truth

Before making code or architecture changes, read the relevant specification documents.

Start with:

- `docs/spec/000-tokenio-gateway.ru.md`

Then read the documents for the layer you are changing:

- External API: `docs/spec/010-external-api.ru.md`
- Auth and billing identity: `docs/spec/020-auth-and-billing-identity.ru.md`
- Routing, resellers, provider types: `docs/spec/030-routing-and-resellers.ru.md`
- Pricing, usage extraction, token accounting: `docs/spec/040-pricing-and-usage.ru.md`
- Local ledger and auto-charge: `docs/spec/050-ledger-and-auto-charge.ru.md`
- Admin API: `docs/spec/060-admin-api.ru.md`
- Database schema: `docs/spec/070-database-schema.ru.md`
- Error model: `docs/spec/080-error-model.ru.md`
- Runtime configuration: `docs/spec/090-configuration.ru.md`
- Native API families: `docs/spec/011-native-api-families.ru.md`

For architecture-changing decisions, also read:

- `docs/adr/`

`README.md` is not the source of truth for behavior. It is only a project overview and run guide.

## Non-negotiable invariants

- Provider-specific behavior must not be implemented in generic gateway layers.
- Every upstream provider is represented through `provider_type`, `reseller`, `route`, `api_family`, `endpoint_kind` and `capabilities`.
- OpenAI-compatible client auth is `Authorization: Bearer sk_...`.
- Native family transport adapters normalize only the explicitly approved family carrier into the same Tokenio `sk_...` credential; carrier policy is defined by `docs/adr/0002-native-api-auth-carriers.md`.
- Client JWT auth is not part of the public API.
- Billing JWT is internal only.
- Request body must not be converted.
- Response body must not be converted.
- Fallback is allowed only inside the same `api_family` and `endpoint_kind`.
- Route key is `api_family + endpoint_kind + client_model`.
- Public `/billing/flush` must not be reintroduced.
- Billing metadata must be returned through headers.
- Reseller API keys are loaded through `api_key_env`.

## Before coding

For every code change:

1. Identify the layer being changed.
2. Verify that the change follows the Go architecture layers and dependency direction from this file.
3. Read `docs/spec/000-tokenio-gateway.ru.md`.
4. Read the specific spec document for that layer.
5. Check relevant ADRs if the change affects architecture.
6. Do not add provider-specific hacks to generic layers.
7. Do not add fallback/legacy/MVP paths.
8. Run:

```bash
gofmt -w .
go test ./...
```

## Go architecture layers

The project uses strict Go layering:

```text
cmd            -> process entrypoints only
internal/app   -> composition root, DI wiring, lifecycle
internal/domain -> entities, value objects, domain errors, pure domain services, domain events
internal/application -> usecases, orchestration, application services
internal/ports -> interfaces/contracts required by application/domain
internal/infrastructure -> implementations of ports: Postgres, HTTP clients, SDK adapters, storage, external APIs
internal/transport -> inbound adapters: HTTP, gRPC, CLI, workers, DTOs, request/response mapping
```

Dependency direction:

```text
cmd            -> app
app            -> transport + application + infrastructure
transport      -> application
application    -> domain + ports
infrastructure -> ports + domain
domain         -> no external dependencies
```

Forbidden dependency direction:

```text
domain -> application
domain -> infrastructure
domain -> transport
application -> infrastructure
application -> transport
ports -> infrastructure
transport -> infrastructure
```

## Dependency Injection and DIP

All concrete dependencies must be created in `internal/app`.

Application code must depend on interfaces, not concrete infrastructure implementations.
Domain code must not depend on infrastructure, transport, application, or external SDKs.

Interfaces must be defined on the consumer side or in `internal/ports` when shared.

Correct:

```text
application defines required interface
infrastructure implements it
app wires concrete implementation into application
```

Wrong:

```text
application imports postgres package directly
domain imports HTTP/SQL/Redis/SDK packages
handlers create repositories or clients directly
global variables hide dependencies
```

## Layer responsibilities

### Domain

Allowed:

```text
entities
value objects
domain errors
domain services
domain events
pure business invariants
```

Forbidden:

```text
SQL
HTTP
JSON DTOs
environment variables
SDK clients
repository implementations
transport status codes
```

### Application

Allowed:

```text
usecases
orchestration
application services
transaction boundaries through ports
calling repositories through interfaces
calling external systems through ports
application-level validation
```

Forbidden:

```text
HTTP handlers
SQL queries
direct env reads
direct SDK usage
provider-specific hacks
concrete infrastructure imports
```

### Ports

Allowed:

```text
repository interfaces
external service interfaces
transaction manager interfaces
secret resolver interfaces
clock/id generator interfaces
```

Rules:

```text
interfaces describe what the consuming layer needs
ports must not depend on infrastructure
ports must not expose concrete database/HTTP/SDK implementation details
```

### Infrastructure

Allowed:

```text
Postgres repositories
HTTP clients
billing client implementation
provider adapters
secret resolver implementation
storage adapters
rate limiter implementation
usage extractor implementation
```

Forbidden:

```text
business orchestration
HTTP handler logic
domain rule ownership
usecase ownership
```

### Transport

Allowed:

```text
HTTP routes
handlers
middleware
request DTOs
response DTOs
request parsing
response mapping
status code mapping
```

Forbidden:

```text
business logic
route selection logic
billing ledger logic
pricing formulas
direct database access
direct provider forwarding
direct env reads
```

### App

Allowed:

```text
config assembly
dependency construction
wiring interfaces to implementations
server lifecycle
startup/shutdown
```

Forbidden:

```text
business rules
request handling logic
pricing formulas
routing decisions
ledger state transitions
```

## Required implementation style

Do not implement business logic in `cmd`, `transport`, or `infrastructure`.

Do not place usecases in `domain`.

Usecases belong to `internal/application`.

Infrastructure implements contracts; it does not define the application architecture.

HTTP handlers must call application usecases only.

Provider-specific behavior must live in provider adapters, not in generic transport/application code.