-- +goose Up
-- Every write gives the basket a new version.
--
-- It is a fresh value rather than a counter so it is never reused: a checkout
-- derives its message id from it, and a basket that is emptied and filled again
-- must not derive an id that was already published - a consumer deduplicating
-- on that id would drop the second order as a repeat of the first.
ALTER TABLE baskets ADD COLUMN IF NOT EXISTS version UUID NOT NULL DEFAULT gen_random_uuid();

-- +goose Down
ALTER TABLE baskets DROP COLUMN IF EXISTS version;
