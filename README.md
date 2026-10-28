# golang-microservices-ecommerce

An e-commerce microservices project written in Go. The domain is split across
five services; each owns its data and uses the persistence technology its
problem calls for.

## Services

| Service | Responsibility | Storage | Status |
| --- | --- | --- | --- |
| Catalog | Product catalog | PostgreSQL | ready |
| Basket | Shopping basket | PostgreSQL + Redis | ready |
| Discount | Coupons | SQLite, over gRPC | ready |
| Ordering | Order lifecycle | SQL Server | ready |
| Payment | Holds cards, issues payment tokens | PostgreSQL | ready |
| ordering-worker | Publishes outbox messages | — | ready |
| gateway | One public address, rate limited at the edge | — | ready |
| web | React storefront, talks only to the gateway | — | ready |

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
  service, and only explicit versioned contracts leave it.
- **Cards stay in one service**: Payment keeps the card and issues a token for
  it. Basket, Ordering and every message between them carry the token and the
  last four digits, never the number, the expiry or the security code - and the
  security code is not stored even by Payment.
- **At-least-once delivery with consumer-side deduplication**: repeats are
  recognised by the broker message id. A message that can never succeed - a
  checkout whose total does not match its items - is rejected and moved to an
  error queue (RabbitMQ) or a `.rejected` topic (Kafka) instead of being
  retried forever.
- **Write and read shapes are separate**: a command accepts a payment token, a
  view returns only the last four digits. The payment types redact themselves
  in both their `fmt` and JSON representations, so neither a log line nor a
  response body can carry a token or a card by accident.

See [docs/architecture.md](docs/architecture.md) for why the outbox exists and
what the delivery guarantees actually are, and
[docs/ordering-worker-runbook.md](docs/ordering-worker-runbook.md) for operating
the worker.

## Layout

```
cmd/           one main package per service
web/           React storefront (Vite, TypeScript)
internal/
  platform/    shared code (cqrs, apperr, pagination, validation, httpx, health)
  catalog/  basket/  discount/  payment/
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

Open the storefront on <http://localhost:6106>. It talks only to the gateway on
`6105`; the services keep their own ports for local work - Catalog `6100`,
Basket `6101`, Discount `6102` (gRPC), Ordering `6103`, ordering-worker `6104`
(health only), Payment `6107`. RabbitMQ's management UI is on `15682`. Ports can be changed with environment variables; see the table at
the top of `deploy/compose.yaml`.

To run against Kafka instead:

```bash
MESSAGE_BROKER_PROVIDER=kafka docker compose -f deploy/compose.yaml --profile kafka up --build
```

Dependencies are deliberately required rather than optional: Basket will not
start without Redis or Discount. A missing cache would quietly become slowness
and a missing discount service would quietly become the wrong price - both of
which are noticed long after a deploy.

## The storefront

```bash
cd web
npm install
npm run dev     # with the stack running; /api is forwarded to the gateway
npm run build   # type check and produce the static files nginx serves
```

The checkout form sends the card to the Payment service first and gets a token
back. Only the token and the last four digits go to Basket, so the card never
reaches the basket, the order or the broker.

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
