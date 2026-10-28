-- +goose Up
-- An order no longer holds the card it was paid with. The card lives in the
-- Payment service, and an order keeps the token that service issued for it,
-- the last four digits so a customer can tell which card they used, and the
-- method. Nullable, as the card columns were: an order can be stored without a
-- payment.
ALTER TABLE Orders ADD Payment_PaymentToken NVARCHAR(64) NULL;
ALTER TABLE Orders ADD Payment_CardLast4 NCHAR(4) NULL;

-- Orders placed before the Payment service existed have no token that refers
-- to their card. They keep their last four digits and get a marker token that
-- says where it came from, rather than an empty value that reads like a
-- missing payment.
UPDATE Orders
SET Payment_CardLast4 = RIGHT(REPLACE(Payment_CardNumber, ' ', ''), 4),
    Payment_PaymentToken = CONCAT('legacy-', LOWER(CONVERT(NVARCHAR(36), Id)))
WHERE Payment_CardNumber IS NOT NULL;

ALTER TABLE Orders DROP COLUMN Payment_CardName;
ALTER TABLE Orders DROP COLUMN Payment_CardNumber;
ALTER TABLE Orders DROP COLUMN Payment_Expiration;
ALTER TABLE Orders DROP COLUMN Payment_CVV;

-- +goose Down
-- One way only. This restores the columns' shape so the earlier migrations
-- line up again, but the card data itself is gone: the columns come back
-- empty.
ALTER TABLE Orders ADD Payment_CardName NVARCHAR(50) NULL;
ALTER TABLE Orders ADD Payment_CardNumber NVARCHAR(24) NULL;
ALTER TABLE Orders ADD Payment_Expiration NVARCHAR(10) NULL;
ALTER TABLE Orders ADD Payment_CVV NVARCHAR(3) NULL;

ALTER TABLE Orders DROP COLUMN Payment_CardLast4;
ALTER TABLE Orders DROP COLUMN Payment_PaymentToken;
