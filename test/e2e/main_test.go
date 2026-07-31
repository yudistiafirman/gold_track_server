// Package e2e drives the real HTTP router (via httptest.Server) against a
// real Postgres database, exercising the full handler -> service ->
// repository stack for every endpoint. It is intentionally separate from
// unit tests: it needs a running Postgres (see docker-compose.yml) and is
// slower, but it's the only suite that proves the wiring in internal/app
// actually works end to end.
//
// Run: docker compose up -d postgres && go test ./test/e2e/...
//
// Convention: whenever a new endpoint is added to the API, add its
// scenarios here (new *_test.go file per resource, following
// health_test.go / auth_test.go / users_test.go) — this suite is meant to
// stay a complete map of the API surface, not just cover what existed when
// it was written.
package e2e

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"gold-track-be/internal/app"
	"gold-track-be/internal/config"
	"gold-track-be/internal/logger"
)

var (
	baseURL  string
	testPool *pgxpool.Pool
)

func TestMain(m *testing.M) {
	code, err := run(m)
	if err != nil {
		fmt.Fprintln(os.Stderr, "e2e setup failed:", err)
		os.Exit(1)
	}
	os.Exit(code)
}

func run(m *testing.M) (int, error) {
	if os.Getenv("DB_NAME") == "" {
		os.Setenv("DB_NAME", "gold_track_test")
	}

	cfg, err := config.Load()
	if err != nil {
		return 0, fmt.Errorf("load config: %w", err)
	}

	if err := ensureDatabaseExists(cfg.Database); err != nil {
		return 0, fmt.Errorf("ensure test database: %w", err)
	}
	if err := runMigrations(cfg.Database); err != nil {
		return 0, fmt.Errorf("run migrations: %w", err)
	}

	log := logger.New(cfg.App.Env)
	ctx := context.Background()

	application, err := app.New(ctx, cfg, log)
	if err != nil {
		return 0, fmt.Errorf("wire app: %w", err)
	}
	defer application.Pool.Close()

	testPool = application.Pool

	srv := httptest.NewServer(application.Router)
	defer srv.Close()
	baseURL = srv.URL

	return m.Run(), nil
}

// ensureDatabaseExists connects to the "postgres" maintenance database and
// creates the target test database if it isn't there yet — so `go test`
// works against a bare Postgres instance with no manual setup beyond
// docker-compose up.
func ensureDatabaseExists(dbCfg config.DatabaseConfig) error {
	adminCfg := dbCfg
	adminCfg.Name = "postgres"

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := pgx.Connect(ctx, adminCfg.DSN())
	if err != nil {
		return err
	}
	defer conn.Close(ctx)

	var exists bool
	err = conn.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)`, dbCfg.Name).Scan(&exists)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}

	_, err = conn.Exec(ctx, fmt.Sprintf(`CREATE DATABASE %s`, pgx.Identifier{dbCfg.Name}.Sanitize()))
	return err
}

func runMigrations(dbCfg config.DatabaseConfig) error {
	m, err := migrate.New(migrationsSourceURL(), dbCfg.MigrateDSN())
	if err != nil {
		return err
	}
	defer m.Close()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return err
	}
	return nil
}

func migrationsSourceURL() string {
	_, thisFile, _, _ := runtime.Caller(0)
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")
	return "file://" + filepath.Join(repoRoot, "migrations")
}
