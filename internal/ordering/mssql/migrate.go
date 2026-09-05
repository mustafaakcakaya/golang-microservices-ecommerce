// Package mssql implements Ordering persistence on SQL Server.
package mssql

import (
	"context"
	"database/sql"
	"embed"
	"fmt"

	"github.com/pressly/goose/v3"

	// The driver registers itself under "sqlserver".
	_ "github.com/microsoft/go-mssqldb"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Open connects to SQL Server.
func Open(dsn string) (*sql.DB, error) {
	db, err := sql.Open("sqlserver", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening sql server connection: %w", err)
	}

	return db, nil
}

// Migrate applies pending migrations.
func Migrate(ctx context.Context, db *sql.DB) error {
	goose.SetBaseFS(migrations)
	goose.SetLogger(goose.NopLogger())

	if err := goose.SetDialect("mssql"); err != nil {
		return fmt.Errorf("setting migration dialect: %w", err)
	}

	if err := goose.UpContext(ctx, db, "migrations"); err != nil {
		return fmt.Errorf("applying migrations: %w", err)
	}

	return nil
}
