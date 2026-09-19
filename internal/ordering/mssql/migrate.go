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
	"strings"
	"time"

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

// agentStartAttempts and agentStartInterval bound how long Migrate waits for
// SQL Server Agent: about a minute, which covers a slow start on a busy host
// without hiding a server whose agent never comes up.
const (
	agentStartAttempts = 30
	agentStartInterval = 2 * time.Second
)

// Migrate applies pending migrations.
//
// Enabling change data capture registers jobs with SQL Server Agent, and the
// server accepts logins before its agent has finished starting. A migration
// that arrives in that window is refused with "try again later", so that is
// what happens here - and only for that refusal. Retrying is safe: a migration
// that failed part way is not recorded as applied, and every statement in the
// capture migration is guarded, so running it again from the top is harmless.
func Migrate(ctx context.Context, db *sql.DB) error {
	goose.SetBaseFS(migrations)
	goose.SetLogger(goose.NopLogger())

	if err := goose.SetDialect("mssql"); err != nil {
		return fmt.Errorf("setting migration dialect: %w", err)
	}

	for attempt := 1; ; attempt++ {
		err := goose.UpContext(ctx, db, "migrations")
		if err == nil {
			return nil
		}
		if !agentStarting(err) || attempt == agentStartAttempts {
			return fmt.Errorf("applying migrations: %w", err)
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("applying migrations: %w", ctx.Err())
		case <-time.After(agentStartInterval):
		}
	}
}

// agentStartingError is SQL Server's number for refusing a job operation while
// its agent is still starting.
const agentStartingError = "14258"

// agentStarting reports whether err is that refusal.
//
// The number cannot be read from the driver's error: the capture procedures
// wrap it, so what comes back is 22832, with 14258 only inside the message.
func agentStarting(err error) bool {
	return strings.Contains(err.Error(), agentStartingError)
}
