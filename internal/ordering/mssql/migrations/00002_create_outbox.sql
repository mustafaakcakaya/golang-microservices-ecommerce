-- +goose Up
-- The outbox is what makes publishing atomic with the change it describes.
--
-- A row is written in the same transaction as the order it belongs to, so the
-- two cannot disagree: either both are committed or neither is. Publishing to a
-- broker from inside a transaction cannot give that - the broker has no way to
-- take part in a rollback, so a crash after the publish and before the commit
-- would announce an order that does not exist.
--
-- The table is append-only. Nothing updates or deletes a row: the reader tracks
-- how far it has got in its own checkpoint table, which keeps writers out of
-- the reader's way and leaves the history intact for reconciliation.
CREATE TABLE OutboxMessages (
    Id            UNIQUEIDENTIFIER NOT NULL CONSTRAINT PK_OutboxMessages PRIMARY KEY,
    -- The stable wire name of the contract, e.g. 'ordering.order-created'.
    EventType     NVARCHAR(200)    NOT NULL,
    SchemaVersion INT              NOT NULL,
    -- Groups the events of one aggregate; brokers that partition use it as the
    -- key, which is what keeps those events in order.
    AggregateId   NVARCHAR(100)    NOT NULL,
    CorrelationId NVARCHAR(100)    NULL,
    OccurredOnUtc DATETIME2        NOT NULL,
    -- The JSON contract, never a domain or storage type. Card details are not
    -- mapped into any contract, so they cannot appear here.
    Payload       NVARCHAR(MAX)    NOT NULL
);

-- +goose Down
DROP TABLE OutboxMessages;
