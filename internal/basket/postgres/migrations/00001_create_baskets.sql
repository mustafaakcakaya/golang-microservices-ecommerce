-- +goose Up
-- The user name is the identity: one basket per user, replaced on every store.
CREATE TABLE IF NOT EXISTS baskets (
    user_name  TEXT        PRIMARY KEY,
    items      JSONB       NOT NULL DEFAULT '[]'::jsonb,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS baskets;
