# Architecture

Four services, each owning its own data and its own persistence technology.
They talk to each other over HTTP and gRPC for questions that need an answer
now, and over a message broker for facts that others need to know about.

| Service | Responsibility | Storage |
| --- | --- | --- |
| Catalog | Products | PostgreSQL |
| Basket | Shopping baskets | PostgreSQL + Redis |
| Discount | Coupons | SQLite, served over gRPC |
| Ordering | Order lifecycle | SQL Server |
| ordering-worker | Publishes Ordering's outbox | — |

## The problem the Ordering service solves

An order is placed and other services need to hear about it. The obvious
implementation is to save the order and then publish a message, and it is
wrong in a way that only shows up under load or during an incident: the
database and the broker are two systems, and nothing makes the pair atomic.

- Publish first, save second: a crash in between announces an order that does
  not exist.
- Save first, publish second: a crash in between stores an order nobody hears
  about.
- Publish inside the transaction: a broker cannot take part in a rollback, so
  this is the first case wearing a disguise.

There is no ordering of the two writes that fixes it, because the problem is
that they are two writes.

## The transactional outbox

The message is written to a table in the same transaction as the order. Now
there is one write, and it either happens or it does not.

```
POST /orders
      |
      v
+--------------------------------------+
|  one SQL transaction                  |
|    Orders        <- the order         |
|    OrderItems    <- its lines         |
|    OutboxMessages <- what to publish  |
+--------------------------------------+
      |
      | SQL Server's capture job reads the transaction log
      v
  cdc.dbo_OutboxMessages_CT   (the change table)
      |
      | ordering-worker polls
      v
  broker (RabbitMQ or Kafka)
      |
      v
  consumer -> InboxMessages -> the actual work
```

Publishing is then a separate problem, solved by a separate process reading
committed rows.

## Why change data capture rather than polling the table

A worker could poll `OutboxMessages` directly. Change data capture is used
instead because it keeps the readers out of the writers' way: capture reads the
transaction log, not the table, so the worker never competes for rows with the
requests that are writing them, and the outbox stays append-only with no status
column for two parties to fight over.

Only `OutboxMessages` is captured. Capturing the business tables would make
every table's shape part of what other services depend on; capturing the outbox
means only the published contracts are.

Capture is enabled by a migration, so the table and the capture instance that
shadows it always arrive together. The procedures refuse to run inside a
transaction, which is why that migration suppresses the one migrations normally
run in.

## Delivery guarantees

Delivery is **at-least-once**. The worker publishes a message and then records
its position, so a crash between the two sends the message again on the next
run. The alternative ordering - record first, publish second - would lose
messages instead, and a repeat can be recognised while a loss cannot be
recovered.

The position moves only at transaction boundaries. Every outbox row written by
one transaction shares a log position, and the checkpoint is saved only after
all of them have been accepted. Half of a transaction reaching other services
would tell them an order was created without the update that corrected it in
the same breath.

Consumers close the loop. Every message carries the event id as its message id,
and `inbox.Idempotent` skips a message this consumer has already finished. The
work and the completion are two steps, so a crash between them repeats the work;
a handler that cannot tolerate that should write its change and its completion
in one transaction.

## Domain events are not integration events

Inside the Ordering service, placing an order raises a domain event carrying
the aggregate. That event never leaves the process. It is mapped, in one place,
to a versioned contract that does leave:

- `ordering.order-created` v1
- `ordering.order-updated` v1

Contracts are versioned in their package name and never edited in place, since
a consumer that has not been redeployed still reads what it was written
against. A reader that does not know an (event type, version) pair refuses the
message rather than forwarding what it cannot read.

**No payment data leaves the service.** An order carries a card number, an
expiry and a security code; none of them are mapped into any contract, so they
cannot reach the outbox, the broker or a log line. The types enforce it as well
as the mapping does: both the domain's `Payment` and the API's `PaymentInput`
refuse to render themselves, in `fmt` and in JSON, and the read model exposes
only a masked number. Reading an order back over HTTP is not a way to read a
card number out of the database.

## Choosing a broker

The publish path works against one interface, so the broker is an environment
variable rather than a code change:

```
MESSAGE_BROKER_PROVIDER=rabbitmq   # or kafka
```

Both publishers wait for the broker to durably accept a message before
returning. Returning earlier would let the worker record progress past
something the broker can still lose, which is the gap the outbox exists to
close.

RabbitMQ publishes to a durable topic exchange with the event type as the
routing key, persistently. Kafka publishes to one topic per event type, keyed
by the aggregate id so an aggregate's events share a partition and keep their
order, waiting for every in-sync replica.

## Where to look in the code

| Concern | Location |
| --- | --- |
| Order aggregate and its rules | `internal/ordering/domain` |
| Commands, queries, HTTP routes | `internal/ordering/orders` |
| Persistence, migrations, outbox write | `internal/ordering/mssql` |
| Domain event to contract mapping | `internal/ordering/integration` |
| Capture reader, checkpoint, retry | `internal/ordering/outbox` |
| Contracts, envelope, publishers | `internal/platform/messaging` |
| Consumer-side deduplication | `internal/platform/messaging/inbox` |
