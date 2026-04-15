package testhelpers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// TestDB wraps a pgxpool.Pool connected to a disposable PostgreSQL container.
type TestDB struct {
	Pool      *pgxpool.Pool
	container testcontainers.Container
}

// NewTestDB starts a PostgreSQL 16 container, applies all goose migrations,
// and returns a ready-to-use pool. The container is terminated when the test
// finishes via t.Cleanup.
func NewTestDB(t *testing.T) *TestDB {
	t.Helper()
	ctx := context.Background()

	pgContainer, err := postgres.Run(ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("governance_test"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("failed to start postgres container: %v", err)
	}

	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("failed to get connection string: %v", err)
	}

	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}

	if err := applyMigrations(ctx, pool); err != nil {
		t.Fatalf("failed to apply migrations: %v", err)
	}

	t.Cleanup(func() {
		pool.Close()
		if err := pgContainer.Terminate(ctx); err != nil {
			t.Logf("warning: failed to terminate postgres container: %v", err)
		}
	})

	return &TestDB{Pool: pool, container: pgContainer}
}

func applyMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	migrationsDir := findMigrationsDir()

	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return fmt.Errorf("reading migrations dir %s: %w", migrationsDir, err)
	}

	var files []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sql") {
			files = append(files, filepath.Join(migrationsDir, e.Name()))
		}
	}
	sort.Strings(files)

	for _, f := range files {
		content, err := os.ReadFile(f)
		if err != nil {
			return fmt.Errorf("reading %s: %w", f, err)
		}

		upSQL := extractGooseUp(string(content))
		if upSQL == "" {
			continue
		}
		if _, err := pool.Exec(ctx, upSQL); err != nil {
			return fmt.Errorf("executing %s: %w", filepath.Base(f), err)
		}
	}
	return nil
}

func extractGooseUp(content string) string {
	parts := strings.SplitN(content, "-- +goose Down", 2)
	if len(parts) == 0 {
		return ""
	}
	up := parts[0]
	up = strings.Replace(up, "-- +goose Up", "", 1)
	// Remove goose statement markers if present
	up = strings.ReplaceAll(up, "-- +goose StatementBegin", "")
	up = strings.ReplaceAll(up, "-- +goose StatementEnd", "")
	return strings.TrimSpace(up)
}

func findMigrationsDir() string {
	// Walk up from cwd to find go.mod (project root)
	dir, err := os.Getwd()
	if err != nil {
		return "migrations/postgres"
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Join(dir, "migrations", "postgres")
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	// Fallback: try common relative paths from test directories
	candidates := []string{
		"../../../migrations/postgres",
		"../../../../migrations/postgres",
		"../../../../../migrations/postgres",
	}
	for _, c := range candidates {
		abs, _ := filepath.Abs(c)
		if _, err := os.Stat(abs); err == nil {
			return abs
		}
	}

	return "migrations/postgres"
}
