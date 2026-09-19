-- +goose Up
-- An order can be placed before it is paid for: a checkout carries no card
-- details, so the order it produces starts out unpaid and is only a draft until
-- a payment is recorded against it. The columns have to be able to say "not
-- yet" rather than holding an empty string that reads like a real card.
ALTER TABLE Orders ALTER COLUMN Payment_CardNumber NVARCHAR(24) NULL;
ALTER TABLE Orders ALTER COLUMN Payment_Expiration NVARCHAR(10) NULL;
ALTER TABLE Orders ALTER COLUMN Payment_CVV NVARCHAR(3) NULL;
ALTER TABLE Orders ALTER COLUMN Payment_PaymentMethod INT NULL;

-- +goose Down
-- Rows without a payment cannot be made to satisfy the old constraint, so they
-- are given an empty payment rather than blocking the rollback.
UPDATE Orders
SET Payment_CardNumber = COALESCE(Payment_CardNumber, ''),
    Payment_Expiration = COALESCE(Payment_Expiration, ''),
    Payment_CVV = COALESCE(Payment_CVV, ''),
    Payment_PaymentMethod = COALESCE(Payment_PaymentMethod, 0);

ALTER TABLE Orders ALTER COLUMN Payment_CardNumber NVARCHAR(24) NOT NULL;
ALTER TABLE Orders ALTER COLUMN Payment_Expiration NVARCHAR(10) NOT NULL;
ALTER TABLE Orders ALTER COLUMN Payment_CVV NVARCHAR(3) NOT NULL;
ALTER TABLE Orders ALTER COLUMN Payment_PaymentMethod INT NOT NULL;
