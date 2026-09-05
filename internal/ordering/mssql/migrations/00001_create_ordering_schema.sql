-- +goose Up
CREATE TABLE Customers (
    Id             UNIQUEIDENTIFIER NOT NULL CONSTRAINT PK_Customers PRIMARY KEY,
    Name           NVARCHAR(100)    NOT NULL,
    Email          NVARCHAR(255)    NOT NULL,
    CreatedAt      DATETIME2        NULL,
    CreatedBy      NVARCHAR(100)    NULL,
    LastModifiedAt DATETIME2        NULL,
    LastModifiedBy NVARCHAR(100)    NULL
);

CREATE TABLE Products (
    Id             UNIQUEIDENTIFIER NOT NULL CONSTRAINT PK_Products PRIMARY KEY,
    Name           NVARCHAR(100)    NOT NULL,
    -- Money is decimal, never float: a binary float cannot represent 0.10 and
    -- the error compounds across order lines.
    Price          DECIMAL(18, 2)   NOT NULL,
    CreatedAt      DATETIME2        NULL,
    CreatedBy      NVARCHAR(100)    NULL,
    LastModifiedAt DATETIME2        NULL,
    LastModifiedBy NVARCHAR(100)    NULL
);

-- Addresses and payment are stored inline rather than in their own tables:
-- they are value objects with no identity of their own and are only ever read
-- as part of the order.
CREATE TABLE Orders (
    Id                          UNIQUEIDENTIFIER NOT NULL CONSTRAINT PK_Orders PRIMARY KEY,
    CustomerId                  UNIQUEIDENTIFIER NOT NULL,
    OrderName                   NVARCHAR(100)    NOT NULL,
    Status                      NVARCHAR(20)     NOT NULL CONSTRAINT DF_Orders_Status DEFAULT 'Draft',

    ShippingAddress_FirstName   NVARCHAR(50)     NOT NULL,
    ShippingAddress_LastName    NVARCHAR(50)     NOT NULL,
    ShippingAddress_EmailAddress NVARCHAR(50)    NULL,
    ShippingAddress_AddressLine NVARCHAR(180)    NOT NULL,
    ShippingAddress_Country     NVARCHAR(50)     NOT NULL,
    ShippingAddress_State       NVARCHAR(50)     NOT NULL,
    ShippingAddress_ZipCode     NVARCHAR(5)      NOT NULL,

    BillingAddress_FirstName    NVARCHAR(50)     NOT NULL,
    BillingAddress_LastName     NVARCHAR(50)     NOT NULL,
    BillingAddress_EmailAddress NVARCHAR(50)     NULL,
    BillingAddress_AddressLine  NVARCHAR(180)    NOT NULL,
    BillingAddress_Country      NVARCHAR(50)     NOT NULL,
    BillingAddress_State        NVARCHAR(50)     NOT NULL,
    BillingAddress_ZipCode      NVARCHAR(5)      NOT NULL,

    Payment_CardName            NVARCHAR(50)     NULL,
    Payment_CardNumber          NVARCHAR(24)     NOT NULL,
    Payment_Expiration          NVARCHAR(10)     NOT NULL,
    Payment_CVV                 NVARCHAR(3)      NOT NULL,
    Payment_PaymentMethod       INT              NOT NULL,

    CreatedAt                   DATETIME2        NULL,
    CreatedBy                   NVARCHAR(100)    NULL,
    LastModifiedAt              DATETIME2        NULL,
    LastModifiedBy              NVARCHAR(100)    NULL,

    CONSTRAINT FK_Orders_Customers FOREIGN KEY (CustomerId) REFERENCES Customers (Id)
);

CREATE INDEX IX_Orders_CustomerId ON Orders (CustomerId);
-- Orders are looked up by name as well as by id.
CREATE INDEX IX_Orders_OrderName ON Orders (OrderName);

CREATE TABLE OrderItems (
    Id             UNIQUEIDENTIFIER NOT NULL CONSTRAINT PK_OrderItems PRIMARY KEY,
    OrderId        UNIQUEIDENTIFIER NOT NULL,
    ProductId      UNIQUEIDENTIFIER NOT NULL,
    Quantity       INT              NOT NULL,
    Price          DECIMAL(18, 2)   NOT NULL,
    CreatedAt      DATETIME2        NULL,
    CreatedBy      NVARCHAR(100)    NULL,
    LastModifiedAt DATETIME2        NULL,
    LastModifiedBy NVARCHAR(100)    NULL,

    -- Lines belong to the order aggregate and have no life without it, so
    -- deleting an order takes them with it.
    CONSTRAINT FK_OrderItems_Orders FOREIGN KEY (OrderId) REFERENCES Orders (Id) ON DELETE CASCADE,
    CONSTRAINT FK_OrderItems_Products FOREIGN KEY (ProductId) REFERENCES Products (Id)
);

CREATE INDEX IX_OrderItems_OrderId ON OrderItems (OrderId);
CREATE INDEX IX_OrderItems_ProductId ON OrderItems (ProductId);

-- +goose Down
DROP TABLE OrderItems;
DROP TABLE Orders;
DROP TABLE Products;
DROP TABLE Customers;
