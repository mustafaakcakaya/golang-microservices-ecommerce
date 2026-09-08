-- +goose NO TRANSACTION
--
-- The CDC procedures refuse to run inside a transaction, and migrations are
-- wrapped in one by default. Suppressing it here is what lets the capture setup
-- live in a migration instead of a deployment script somebody has to remember
-- to run - the schema and the capture instance that shadows it then always
-- arrive together.
--
-- Requires an edition that supports change data capture (Developer, Standard or
-- Enterprise - not Express) and a login with db_owner rights. SQL Server Agent
-- must be running for the capture job to actually copy changes into the change
-- table; without it, capture is enabled and nothing ever appears.

-- +goose Up
-- +goose StatementBegin
IF (SELECT is_cdc_enabled FROM sys.databases WHERE name = DB_NAME()) = 0
    EXEC sys.sp_cdc_enable_db;
-- +goose StatementEnd

-- Only OutboxMessages is captured. Capturing the business tables would make
-- every table's shape part of what other services depend on; capturing the
-- outbox means only the contracts are.
-- +goose StatementBegin
IF NOT EXISTS (SELECT 1 FROM cdc.change_tables WHERE capture_instance = N'dbo_OutboxMessages')
    EXEC sys.sp_cdc_enable_table
        @source_schema = N'dbo',
        @source_name = N'OutboxMessages',
        @capture_instance = N'dbo_OutboxMessages',
        @role_name = NULL,
        -- The outbox is append-only, so there is no "net" of several changes to
        -- one row to compute; every row is one insert.
        @supports_net_changes = 0;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
IF EXISTS (SELECT 1 FROM cdc.change_tables WHERE capture_instance = N'dbo_OutboxMessages')
    EXEC sys.sp_cdc_disable_table
        @source_schema = N'dbo',
        @source_name = N'OutboxMessages',
        @capture_instance = N'dbo_OutboxMessages';
-- +goose StatementEnd

-- +goose StatementBegin
IF (SELECT is_cdc_enabled FROM sys.databases WHERE name = DB_NAME()) = 1
    EXEC sys.sp_cdc_disable_db;
-- +goose StatementEnd
