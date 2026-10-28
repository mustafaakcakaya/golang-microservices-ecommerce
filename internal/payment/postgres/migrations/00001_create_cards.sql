-- +goose Up
-- The cards this service has issued tokens for.
--
-- There is no column for the security code, on purpose: it is checked when a
-- card arrives and then dropped, so there is no place it could be kept by
-- mistake.
--
-- The number is stored as it was given. A real system would encrypt it, with
-- the key held outside the database, or hand it to a payment provider's vault
-- and keep only the provider's reference. This one never sees a real card.
CREATE TABLE IF NOT EXISTS cards (
    -- The token other services know the card by.
    token       TEXT        PRIMARY KEY,
    card_number TEXT        NOT NULL,
    expiration  TEXT        NOT NULL,
    card_name   TEXT        NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS cards;
