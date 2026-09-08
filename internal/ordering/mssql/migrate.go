// Package mssql implements Ordering persistence on SQL Server.
package mssql

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"net/url"
	"regexp"

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

// plainIdentifier matches the database names this is willing to create.
var plainIdentifier = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,62}$`)

// EnsureDatabase creates the database named in the DSN if the server does not
// have it yet.
//
// A fresh server has only its system databases, and Ordering needs one of its
// own rather than master: change data capture cannot be enabled on a system
// database, so the outbox pipeline would have nowhere to live.
func EnsureDatabase(ctx context.Context, dsn string) error {
	parsed, err := url.Parse(dsn)
	if err != nil {
		return fmt.Errorf("parsing connection string: %w", err)
	}

	name := parsed.Query().Get("database")
	if name == "" {
		return errors.New("the connection string must name a database")
	}
	// An identifier cannot be a bound parameter, so the name is checked here
	// as well as quoted by the server below.
	if !plainIdentifier.MatchString(name) {
		return fmt.Errorf("database name %q is not a plain identifier", name)
	}

	db, err := Open(masterDSN(parsed))
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	const statement = `
		IF DB_ID(@p1) IS NULL
		BEGIN
			DECLARE @create NVARCHAR(MAX) = N'CREATE DATABASE ' + QUOTENAME(@p1);
			EXEC sp_executesql @create;
		END`

	if _, err := db.ExecContext(ctx, statement, name); err != nil {
		return fmt.Errorf("creating database %s: %w", name, err)
	}

	return nil
}

// masterDSN points the same credentials at master, the only database that is
// certain to exist.
func masterDSN(parsed *url.URL) string {
	admin := *parsed

	query := admin.Query()
	query.Set("database", "master")
	admin.RawQuery = query.Encode()

	return admin.String()
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
