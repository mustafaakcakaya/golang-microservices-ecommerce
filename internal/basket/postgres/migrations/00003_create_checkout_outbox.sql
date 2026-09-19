-- +goose Up
-- A checkout waiting to be published.
--
-- The row is written in the same transaction that deletes the basket, so a
-- checkout either happens completely - basket gone and the announcement
-- certain to go out - or not at all. Publishing is the relay's job, never the
-- request's: a broker outage then delays the announcement instead of failing a
-- checkout whose database work was perfectly fine.
CREATE TABLE IF NOT EXISTS basket_outbox_messages (
    -- The event id, which travels to the broker as the message id. Derived
    -- from the basket version, so two racing checkouts write one row.
    id                  UUID        PRIMARY KEY,
    event_type          TEXT        NOT NULL,
    schema_version      INT         NOT NULL,
    aggregate_id        TEXT        NOT NULL,
    correlation_id      TEXT,
    occurred_on_utc     TIMESTAMPTZ NOT NULL,
    -- Text rather than JSONB: the payload is forwarded to the broker exactly
    -- as it was written, and JSONB would rewrite it on the way through.
    payload             TEXT        NOT NULL,
    attempts            INT         NOT NULL DEFAULT 0,
    -- When the relay may next try. Failures push it out, which is how the
    -- backoff survives a restart.
    next_attempt_on_utc TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_error          TEXT,
    -- Set once a message has failed too often; the relay then leaves it alone
    -- so one message nobody can publish cannot block the rest.
    poisoned_on_utc     TIMESTAMPTZ
);

-- The relay's only query: what is due, oldest first.
CREATE INDEX IF NOT EXISTS ix_basket_outbox_due
    ON basket_outbox_messages (next_attempt_on_utc)
    WHERE poisoned_on_utc IS NULL;

-- +goose Down
DROP TABLE IF EXISTS basket_outbox_messages;
