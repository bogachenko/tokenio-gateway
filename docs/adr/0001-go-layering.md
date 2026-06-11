# 0001. Go layering and dependency rules

## Status

Accepted

## Purpose

This document defines the mandatory Go application architecture for Tokenio Gateway.

The project uses explicit layers, Dependency Injection, and Dependency Inversion Principle.

## Target package layout

```text
cmd/
  gateway/
    main.go

internal/
  app/
    app.go
    config.go

  domain/
    entities
    value_objects
    errors
    events
    services

  application/
    usecases
    services
    orchestration

  ports/
    repositories
    services
    gateways
    clock
    ids
    secrets
    transactions

  infrastructure/
    postgres
    billing
    forwarding
    providers
    pricing
    secrets
    ratelimit

  transport/
    http
    dto
    middleware
```

## Dependency direction

Allowed dependency direction:

```text
cmd -> app
app -> transport
app -> application
app -> infrastructure
app -> ports
transport -> application
application -> domain
application -> ports
infrastructure -> domain
infrastructure -> ports
```

Forbidden dependency direction:

```text
domain -> application
domain -> ports
domain -> infrastructure
domain -> transport
application -> infrastructure
application -> transport
ports -> infrastructure
ports -> transport
transport -> infrastructure
```

## Domain layer

Domain contains business concepts that are true independently of infrastructure.

Allowed:

```text
entities
value objects
domain errors
domain events
pure domain services
business invariants
```

Forbidden:

```text
SQL
HTTP
JSON DTOs
environment variables
configuration loading
provider SDKs
billing clients
repository implementations
transport status codes
```

## Application layer

Application contains usecases and orchestration.

Allowed:

```text
usecase execution
loading entities through ports
saving entities through ports
calling external systems through ports
transaction boundaries through ports
application-level validation
ledger orchestration
route selection orchestration
billing orchestration
```

Forbidden:

```text
SQL queries
HTTP handlers
provider-specific JSON parsing
direct env reads
direct SDK usage
concrete infrastructure imports
```

## Ports layer

Ports define interfaces required by inner layers.

Interfaces must be owned by the consumer side.

Allowed:

```text
UserRepository
APIKeyRepository
RouteRepository
UsageLedger
BillingClient
ForwardingAdapter
UsageExtractor
TokenEstimator
SecretResolver
TransactionManager
Clock
IDGenerator
```

Rules:

```text
ports describe behavior needed by application/domain
ports must not mirror concrete infrastructure structs
ports must not expose raw secrets
ports must not depend on Postgres/Redis/HTTP SDK implementation details
```

## Infrastructure layer

Infrastructure implements ports.

Allowed:

```text
Postgres repositories
HTTP clients
billing client
provider adapters
secret resolver
rate limiter
token estimator
usage extractors
error classifiers
```

Forbidden:

```text
owning business rules
owning usecase orchestration
writing HTTP responses
parsing public HTTP requests
changing domain invariants
```

## Transport layer

Transport is an inbound adapter.

Allowed:

```text
HTTP server
routes
handlers
middleware
request DTOs
response DTOs
request parsing
response mapping
error-to-status mapping
```

Forbidden:

```text
business logic
route selection
pricing calculation
ledger state transitions
direct database access
direct provider calls
direct billing calls
direct env reads
```

## App layer

App is the composition root.

Allowed:

```text
construct config
construct DB connection
construct repositories
construct infrastructure clients
construct usecases
construct HTTP server
wire interfaces to implementations
start and stop lifecycle
```

Forbidden:

```text
business rules
request processing
pricing formulas
route selection algorithm
ledger state machine
provider-specific parsing
```

## Dependency Injection rule

Concrete implementations are created in `internal/app`.

Application constructors receive interfaces.

Example:

```go
type CancelOrderUseCase struct {
    orders OrderRepository
}

func NewCancelOrderUseCase(orders OrderRepository) *CancelOrderUseCase {
    return &CancelOrderUseCase{orders: orders}
}
```

The concrete implementation is wired only in app:

```go
orderRepo := postgres.NewOrderRepository(db)
cancelOrder := application.NewCancelOrderUseCase(orderRepo)
```

## Interface ownership rule

Correct:

```text
application defines the repository interface it needs
infrastructure implements that interface
app wires the concrete implementation
```

Wrong:

```text
postgres package defines UserRepositoryInterface
application imports postgres package
domain imports infrastructure package
```

## Configuration rule

Environment variables are read only in:

```text
internal/config
internal/app bootstrap
dedicated SecretResolver implementation
admin diagnostic env presence check
```

Forbidden:

```text
os.Getenv in handlers
os.Getenv in usecases
os.Getenv in domain
os.Getenv in route selector
os.Getenv in pricing calculator
```

## Tokenio-specific architectural rules

Tokenio Gateway is a forwarding, billing, routing gateway.

It is not an SDK converter.

Mandatory invariants:

```text
request API format is preserved
semantic request payload is not converted
only explicit model identifier rewrite is allowed
response body is not converted
billing metadata goes to headers
route lookup uses api_family + endpoint_kind + client_model
fallback never crosses api_family
provider-specific logic lives in provider adapters
generic layers work only with normalized metadata
local ledger is source of truth for usage
Postgres is source of truth for persisted state
```

## Review rule

Any code change must be rejected if it:

```text
mixes transport/application/domain/infrastructure responsibilities
adds provider-specific behavior to generic HTTP/application code
adds fallback between API families
uses model-only route selection
stores or logs raw secrets
uses SHA-256 without HMAC secret for user API keys
creates billing without local ledger record
mutates semantic request payload outside explicit model rewrite
adds public billing flush endpoint
```
