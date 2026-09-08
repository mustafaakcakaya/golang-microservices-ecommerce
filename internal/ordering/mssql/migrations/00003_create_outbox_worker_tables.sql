-- +goose Up
-- How far the reader has got. It lives beside the outbox rather than in the
-- worker, so restarting or replacing the worker does not replay the history.
--
-- Keeping it out of OutboxMessages is deliberate: the outbox is written by the
-- request that produced the message and read by the worker, and a status column
-- would put the two in each other's way on the same rows.
CREATE TABLE OutboxCdcCheckpoints (
    -- Named per reader, so a second independent reader could be added without
    -- either of them moving the other's position.
    ConsumerName     NVARCHAR(100) NOT NULL CONSTRAINT PK_OutboxCdcCheckpoints PRIMARY KEY,
    -- A log sequence number: SQL Server's position in its own transaction log.
    LastProcessedLsn BINARY(10)    NOT NULL,
    UpdatedOnUtc     DATETIME2     NOT NULL
);

-- Publish attempts that failed.
--
-- A message the broker keeps refusing must not block every message behind it,
-- so after enough attempts it is marked poisoned and the stream moves on. The
-- row is the record of what was skipped and why.
CREATE TABLE OutboxPublishFailures (
    OutboxMessageId  UNIQUEIDENTIFIER NOT NULL CONSTRAINT PK_OutboxPublishFailures PRIMARY KEY,
    Attempts         INT              NOT NULL,
    LastError        NVARCHAR(2000)   NULL,
    LastAttemptOnUtc DATETIME2        NOT NULL,
    PoisonedOnUtc    DATETIME2        NULL
);

-- Filtered: the question worth an index is "what was given up on", and poisoned
-- messages are the rare case.
CREATE INDEX IX_OutboxPublishFailures_PoisonedOnUtc
    ON OutboxPublishFailures (PoisonedOnUtc)
    WHERE PoisonedOnUtc IS NOT NULL;

-- What a consumer has already handled.
--
-- Delivery is at-least-once: the worker publishes and then records its
-- position, so a crash between the two re-delivers. That is the safe direction
-- to fail, and this table is the other half of the bargain - the consumer, not
-- the publisher, is what makes the effect happen once.
CREATE TABLE InboxMessages (
    MessageId      UNIQUEIDENTIFIER NOT NULL,
    -- Part of the key: two consumers must each get their own chance at a
    -- message, so one having handled it says nothing about the other.
    ConsumerName   NVARCHAR(200)    NOT NULL,
    ReceivedOnUtc  DATETIME2        NOT NULL,
    ProcessedOnUtc DATETIME2        NULL,
    CONSTRAINT PK_InboxMessages PRIMARY KEY (MessageId, ConsumerName)
);

-- +goose Down
DROP TABLE InboxMessages;
DROP INDEX IX_OutboxPublishFailures_PoisonedOnUtc ON OutboxPublishFailures;
DROP TABLE OutboxPublishFailures;
DROP TABLE OutboxCdcCheckpoints;
