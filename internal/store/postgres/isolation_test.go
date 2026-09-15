package postgres_test

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// schemaNamePattern is the whitelist every generated schema name must
// match before it is interpolated into DDL. A schema name cannot be a
// bound parameter — PostgreSQL has no placeholder for an identifier — so
// the only safe construction is one where the value is generated here and
// provably contains nothing but lowercase letters, digits, and
// underscores. This check exists so that stays true even if the
// generator below is changed later.
var schemaNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,48}$`)

// isolatedSchemaDSN creates a PostgreSQL schema unique to this test and
// returns a DSN whose search_path points at it, so everything the store
// creates and writes lives there and nowhere else. The schema is dropped
// when the test finishes.
//
// This is the mechanism that lets two packages' test binaries — which
// `go test ./...` runs concurrently — share one database without either
// disturbing the other, and it replaces an earlier shared-table TRUNCATE
// that could not do that. See newStore's comment for the failure that
// motivated it.
func isolatedSchemaDSN(t testing.TB, dsn string) string {
	t.Helper()

	name := fmt.Sprintf("tv_test_%d_%d", os.Getpid(), time.Now().UnixNano())
	if !schemaNamePattern.MatchString(name) {
		t.Fatalf("generated schema name %q is not a safe identifier", name)
	}

	ctx := context.Background()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("pgx.Connect() error = %v", err)
	}
	defer admin.Close(ctx)

	if _, err := admin.Exec(ctx, `CREATE SCHEMA `+name); err != nil {
		t.Fatalf("CREATE SCHEMA %s error = %v", name, err)
	}

	t.Cleanup(func() {
		// A fresh connection: the one above is closed by the defer, and
		// the store's pool may still be closing when this runs.
		cleanupCtx := context.Background()
		conn, err := pgx.Connect(cleanupCtx, dsn)
		if err != nil {
			t.Errorf("cleanup: pgx.Connect() error = %v", err)
			return
		}
		defer conn.Close(cleanupCtx)
		if _, err := conn.Exec(cleanupCtx, `DROP SCHEMA `+name+` CASCADE`); err != nil {
			t.Errorf("cleanup: DROP SCHEMA %s error = %v", name, err)
		}
	})

	return withSearchPath(t, dsn, name)
}

// withSearchPath returns dsn with a search_path startup option pointing
// at schema. It goes through net/url rather than string concatenation so
// a DSN that already carries query parameters (sslmode, connect_timeout)
// keeps them, and so the option value is escaped correctly.
func withSearchPath(t testing.TB, dsn, schema string) string {
	t.Helper()

	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("url.Parse(dsn) error = %v", err)
	}
	q := u.Query()
	// "-csearch_path=x", not "-c search_path=x": url.Values.Encode escapes
	// a space as "+", and libpq splits the options string on whitespace
	// without URL-decoding it, so the "+" arrives as part of the parameter
	// name and the server rejects it. The no-space form avoids the
	// question entirely.
	q.Set("options", "-csearch_path="+schema)
	u.RawQuery = q.Encode()
	return u.String()
}

// withApplicationName returns dsn with application_name set, so a test can
// identify — and act on — only the connections its own store opened.
// pg_stat_activity exposes application_name, which makes it the one
// reliable way to distinguish "my connections" from every other
// concurrently-running test package's.
func withApplicationName(t testing.TB, dsn, name string) string {
	t.Helper()

	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("url.Parse(dsn) error = %v", err)
	}
	q := u.Query()
	q.Set("application_name", name)
	u.RawQuery = q.Encode()
	return u.String()
}
