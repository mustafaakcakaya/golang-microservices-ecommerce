-- +goose Up
-- An order is identified by its id alone. The name only ever copied a value
-- that came with the checkout, and nothing reads it any more.
DROP INDEX IX_Orders_OrderName ON Orders;
ALTER TABLE Orders DROP COLUMN OrderName;

-- +goose Down
-- One way only. This restores the column's shape so the earlier migrations
-- line up again, but the names themselves are gone: every order comes back
-- with an empty one.
ALTER TABLE Orders ADD OrderName NVARCHAR(100) NOT NULL
    CONSTRAINT DF_Orders_OrderName DEFAULT '';
ALTER TABLE Orders DROP CONSTRAINT DF_Orders_OrderName;
CREATE INDEX IX_Orders_OrderName ON Orders (OrderName);
