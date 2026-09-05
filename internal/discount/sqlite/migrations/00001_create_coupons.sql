-- +goose Up
CREATE TABLE IF NOT EXISTS coupons (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    product_name TEXT    NOT NULL,
    description  TEXT    NOT NULL DEFAULT '',
    amount       INTEGER NOT NULL DEFAULT 0
);

-- Lookups are by product name, and the service treats it as the business key:
-- GetDiscount and DeleteDiscount both address a coupon that way, so two rows
-- for one product would make the answer depend on row order.
CREATE UNIQUE INDEX IF NOT EXISTS ux_coupons_product_name ON coupons (product_name);

-- +goose Down
DROP INDEX IF EXISTS ux_coupons_product_name;
DROP TABLE IF EXISTS coupons;
