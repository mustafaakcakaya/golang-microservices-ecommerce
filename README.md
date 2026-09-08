# golang-microservices-ecommerce

An e-commerce microservices project written in Go. The domain is split across
four services; each owns its data and uses the persistence technology its
problem calls for.

## Services

| Service | Responsibility | Storage | Status |
| --- | --- | --- | --- |
| Catalog | Product catalog | PostgreSQL | ready |
| Basket | Shopping basket | PostgreSQL + Redis | ready |
| Discount | Coupons | SQLite, over gRPC | ready |
| Ordering | Order lifecycle | SQL Server | ready |
| ordering-worker | Publishes outbox messages | — | ready |

## Architectural approaches

- **Clean Architecture** (Ordering): dependencies point inwards.
  `internal/ordering/domain` imports no infrastructure package, and the linter
  enforces that rather than leaving it to good intentions.
- **Domain-Driven Design**: `Order` is an aggregate root, with value objects and
  domain events.
- **CQRS**: commands and queries are separate handlers, with cross-cutting
  concerns added as middleware. There is no dispatcher; handlers are wired
  explicitly in `main`.
- **Vertical slices**: each feature keeps its request, handler and HTTP route in
  one file, so a behaviour is read and changed in one place.
- **Transactional outbox + change data capture**: the aggregate change and the
  message announcing it are written in the same SQL transaction; a separate
  worker reads them through SQL Server's change tables and publishes them.
- **Domain events are not integration events**: domain events stay inside the
  service, and only explicit versioned contracts leave it. Payment data - card
  numbers, security codes - never reaches the outbox, a log or the broker.
- **At-least-once delivery with consumer-side deduplication**: repeats are
  recognised by the broker message id.
- **Write and read shapes are separate**: a command accepts a card, a view never
  returns one. The payment types redact themselves in both their `fmt` and JSON
  representations, so neither a log line nor a response body can carry a card
  number by accident.

See [docs/architecture.md](docs/architecture.md) for why the outbox exists and
what the delivery guarantees actually are, and
[docs/ordering-worker-runbook.md](docs/ordering-worker-runbook.md) for operating
the worker.

## Layout

```
cmd/           one main package per service
internal/
  platform/    shared code (cqrs, apperr, pagination, validation, httpx, health)
  catalog/  basket/  discount/
  ordering/
    domain/       aggregate, value objects, domain events - no outside dependencies
    orders/       command and query handlers, request and view contracts, routes
    mssql/        persistence, migrations, outbox writes, inbox store
    integration/  domain events mapped to published contracts
    outbox/       capture reader, checkpoint, retry and poison handling
    api/          router
  platform/messaging/   contracts, envelope, publishers, broker switch, inbox
proto/         gRPC contracts between services
deploy/        compose and deployment definitions
```

A single `go.mod` is used: the services are developed and deployed together, and
the version-pinning overhead of multiple modules would not pay for itself at
this stage.

## Running

```bash
docker compose -f deploy/compose.yaml up --build
```

Service addresses: Catalog `6100`, Basket `6101`, Discount `6102` (gRPC),
Ordering `6103`, ordering-worker `6104` (health only). RabbitMQ's management UI
is on `15682`. Ports can be changed with environment variables; see the table at
the top of `deploy/compose.yaml`.

To run against Kafka instead:

```bash
MESSAGE_BROKER_PROVIDER=kafka docker compose -f deploy/compose.yaml --profile kafka up --build
```

Dependencies are deliberately required rather than optional: Basket will not
start without Redis or Discount. A missing cache would quietly become slowness
and a missing discount service would quietly become the wrong price - both of
which are noticed long after a deploy.

## Development

```bash
make build     # compile every package
make test      # run the tests with the race detector
make lint      # golangci-lint
make proto     # regenerate Go code from the .proto files (needs buf)
```

Go 1.26+ is required. Generated protobuf code is committed, so building does not
need the protobuf toolchain.

Integration tests bring up real PostgreSQL, Redis, SQL Server, RabbitMQ and
Kafka instances with Testcontainers. For the fast subset that needs no Docker,
use `go test -short ./...`.
