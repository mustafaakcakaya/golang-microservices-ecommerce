package mssql

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/pressly/goose/v3"
)

// MigrateTo applies migrations up to and including version, so a test can
// put rows into an older schema and watch a later migration carry them over.
func MigrateTo(ctx context.Context, db *sql.DB, version int64) error {
	goose.SetBaseFS(migrations)
	goose.SetLogger(goose.NopLogger())

	if err := goose.SetDialect("mssql"); err != nil {
		return fmt.Errorf("setting migration dialect: %w", err)
	}

	return goose.UpToContext(ctx, db, "migrations", version)
}
