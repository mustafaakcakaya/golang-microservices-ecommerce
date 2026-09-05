-- +goose Up
-- Products are stored as documents rather than relational rows: nothing in the
-- service: the id is a real column for lookups, everything else is JSONB.
CREATE TABLE IF NOT EXISTS products (
    id   uuid  PRIMARY KEY,
    data jsonb NOT NULL
);

-- GIN index on the category array serves the "category contains X" query
-- through the jsonb ? operator.
CREATE INDEX IF NOT EXISTS products_category_idx
    ON products USING gin ((data -> 'category'));

-- +goose Down
DROP TABLE IF EXISTS products;
