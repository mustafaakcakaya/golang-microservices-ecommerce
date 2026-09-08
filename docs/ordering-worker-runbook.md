# ordering-worker runbook

The worker reads Ordering's outbox out of SQL Server's change tables and
publishes it. It owns no schema: capture and the tables it uses are created by
the Ordering migrations, which the API applies at startup.

## Configuration

| Variable | Default | Notes |
| --- | --- | --- |
| `ORDERING_DATABASE_URL` | required | Carries the password; never in source. |
| `ORDERING_WORKER_HTTP_ADDR` | `:8080` | Health endpoint only. |
| `MESSAGE_BROKER_PROVIDER` | `rabbitmq` | Or `kafka`. |
| `MESSAGE_BROKER_RABBITMQ_URL` | — | Required for RabbitMQ. |
| `MESSAGE_BROKER_RABBITMQ_EXCHANGE` | `integration-events` | Durable topic exchange. |
| `MESSAGE_BROKER_KAFKA_BROKERS` | — | Comma separated. Required for Kafka. |
| `MESSAGE_BROKER_KAFKA_TOPIC_PREFIX` | empty | Prepended to the event type. |
| `OUTBOX_CONSUMER_NAME` | `ordering-outbox-worker` | Owns the checkpoint row. |
| `OUTBOX_BATCH_SIZE` | `100` | Messages per cycle. |
| `OUTBOX_POLL_INTERVAL` | `5s` | Wait when there was nothing to do. |
| `OUTBOX_MAX_PUBLISH_ATTEMPTS` | `10` | Then the message is given up on. |
| `OUTBOX_RETRY_BASE_DELAY` | `1s` | Backoff after a failed cycle. |
| `OUTBOX_RETRY_MAX_DELAY` | `1m` | Backoff ceiling. |

An unusable configuration is refused at startup rather than on the first
message.

## Health

`GET /health` reports three things separately:

- `sqlserver` — the database answers.
- `capture` — capture is enabled and its job has recorded a position.
- `publishing` — the last few cycles did not all fail.

The third is the one that matters. A worker that cannot publish is still
running and still answering, which is exactly what a liveness check misses.

## Normal startup

```
change data capture is ready
outbox worker started
outbox cycle finished  published=3 poisoned=0 checkpoint=0x...
```

## "waiting for change data capture"

Expected briefly after a deployment: the worker can start before the Ordering
migrations have run. The reason says which step is missing.

- *the database does not have change data capture enabled* — the migrations
  have not been applied to this database. Start the API against it, or apply
  them yourself.
- *the capture instance for the outbox table does not exist* — the same, or
  capture was disabled by hand afterwards.
- *the capture job has not recorded a position yet* — capture is enabled but
  SQL Server Agent is not running, so nothing copies changes into the change
  table. Set `MSSQL_AGENT_ENABLED=true` (or start the agent) and restart the
  database. Capture also needs an edition that supports it: Developer,
  Standard or Enterprise, not Express.

If this persists, no messages are being published, but nothing is lost: the
outbox rows are still there and will be read once capture works.

## "publishing an outbox message failed"

One message would not go out. The attempt is counted in
`OutboxPublishFailures` and the position stays where it is, so the message is
read again next cycle. The count survives a restart, which is what stops a
crash-looping worker from retrying forever.

Look at the broker first. If the broker is fine, look at `LastError` in that
table.

```sql
SELECT OutboxMessageId, Attempts, LastAttemptOnUtc, LastError
FROM OutboxPublishFailures
WHERE PoisonedOnUtc IS NULL
ORDER BY Attempts DESC;
```

## "giving up on an outbox message"

After `OUTBOX_MAX_PUBLISH_ATTEMPTS` the message is marked poisoned and the
stream moves past it. This is deliberate: one message nobody can publish must
not stop every message written after it.

Nothing republishes it automatically. To find what was skipped:

```sql
SELECT f.OutboxMessageId, f.Attempts, f.PoisonedOnUtc, f.LastError,
       m.EventType, m.SchemaVersion, m.AggregateId, m.OccurredOnUtc
FROM OutboxPublishFailures f
JOIN OutboxMessages m ON m.Id = f.OutboxMessageId
WHERE f.PoisonedOnUtc IS NOT NULL
ORDER BY f.PoisonedOnUtc DESC;
```

To retry one after fixing the cause, clear its record and rewind the checkpoint
to just before it. Rewinding replays everything after that point as well, which
consumers absorb through the inbox - that is what the inbox is for.

```sql
DELETE FROM OutboxPublishFailures WHERE OutboxMessageId = @id;
```

A common cause is a contract the worker does not know: it refuses to publish an
(event type, version) pair it was not built with, rather than putting a message
on the bus that nothing can explain. Deploy a worker that knows the contract
and the message goes out on the next cycle.

## "the outbox checkpoint is outside the retention window"

The serious one. The stored position is older than the oldest change capture
still keeps, so changes between the two have been cleaned up and messages may
never have been published. The worker refuses to move on, because no automatic
choice here is honest.

It happens when the worker has been stopped for longer than the capture
retention window (three days by default), or the checkpoint was moved by hand.

To recover:

1. Note the checkpoint the log reports and the oldest retained position.
2. Reconcile against `OutboxMessages`, which is append-only and still holds
   every row. Work out which messages fall in the gap - compare against what
   consumers have in their `InboxMessages`, or against the aggregates
   themselves.
3. Republish what is missing.
4. Set the checkpoint to the oldest retained position and restart:

```sql
UPDATE OutboxCdcCheckpoints
SET LastProcessedLsn = sys.fn_cdc_get_min_lsn(N'dbo_OutboxMessages'),
    UpdatedOnUtc = SYSUTCDATETIME()
WHERE ConsumerName = 'ordering-outbox-worker';
```

To avoid it: do not leave the worker stopped for days, or raise the retention
window on the capture cleanup job.

## Switching brokers

Set `MESSAGE_BROKER_PROVIDER` and restart. The checkpoint is unaffected, so the
worker continues from where it was - messages published before the switch are
not resent to the new broker. If consumers are moving too, they need their
existing messages drained from the old broker first.

## Running more than one worker

Do not, for now. Two workers sharing a consumer name would read the same
changes and race on one checkpoint row, publishing duplicates and possibly
moving the position backwards. Duplicates are absorbed by consumers, but a
position moving backwards is not something the design accounts for yet.
Running one instance is a deliberate limitation; see the decision record.
