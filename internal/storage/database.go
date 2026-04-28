package storage

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/amirsalarsafaei/sqlc-pgx-monitoring/dbtracer"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/lib/pq"
	pgxvector "github.com/pgvector/pgvector-go/pgx"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
	"go.opentelemetry.io/otel"
)

//go:embed schema/migrations/*.sql
var migrationFiles embed.FS

type DatabaseConfig struct {
	Host       string `split_words:"true" default:"localhost"`
	Port       int    `split_words:"true" default:"5432"`
	User       string `split_words:"true" default:"postgres"`
	Pass       string `split_words:"true" default:"password"`
	Name       string `split_words:"true" default:"ratchet"`
	DisableTLS bool   `split_words:"true" default:"true"`
}

func (c DatabaseConfig) URL() string {
	dbURL := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(c.User, c.Pass),
		Host:   net.JoinHostPort(c.Host, strconv.Itoa(c.Port)),
		Path:   c.Name,
	}
	if c.DisableTLS {
		dbURL.RawQuery = "sslmode=disable"
	}

	return dbURL.String()
}

func New(ctx context.Context, dbURL string) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		return nil, fmt.Errorf("parsing database config: %w", err)
	}

	tempPool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("connecting to database: %w", err)
	}
	defer tempPool.Close()
	// River migrations
	rm, err := rivermigrate.New(riverpgxv5.New(tempPool), nil)
	if err != nil {
		return nil, fmt.Errorf("creating river migrate: %w", err)
	}

	if _, err = rm.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		return nil, fmt.Errorf("applying river migrations: %w", err)
	}

	// Ratchet migrations
	if err := runMigrations(ctx, dbURL); err != nil {
		return nil, fmt.Errorf("applying migrations: %w", err)
	}

	config.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		return pgxvector.RegisterTypes(ctx, conn)
	}
	tracerProvider := otel.GetTracerProvider()
	tracer, err := dbtracer.NewDBTracer("ratchet",
		dbtracer.WithTraceProvider(tracerProvider),
		dbtracer.WithShouldLog(func(err error) bool {
			return err != nil
		}),
		dbtracer.WithIncludeSpanNameSuffix(true),
	)
	if err != nil {
		return nil, fmt.Errorf("creating db tracer: %w", err)
	}
	config.ConnConfig.Tracer = tracer

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("connecting to database: %w", err)
	}

	slog.Info("Migrations applied successfully!")
	return pool, nil
}

func runMigrations(ctx context.Context, dbURL string) error {
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		return fmt.Errorf("opening migration database: %w", err)
	}
	defer db.Close()

	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("connecting migration database: %w", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version bigint NOT NULL PRIMARY KEY, dirty boolean NOT NULL)`); err != nil {
		return fmt.Errorf("creating schema migrations table: %w", err)
	}

	currentVersion, dirty, err := currentMigrationVersion(ctx, db)
	if err != nil {
		return err
	}
	if dirty {
		return errors.New("schema migrations table is dirty")
	}

	entries, err := fs.ReadDir(migrationFiles, "schema/migrations")
	if err != nil {
		return fmt.Errorf("reading migrations: %w", err)
	}

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".up.sql") {
			continue
		}

		version, err := migrationVersion(name)
		if err != nil {
			return err
		}
		if version <= currentVersion {
			continue
		}

		contents, err := migrationFiles.ReadFile("schema/migrations/" + name)
		if err != nil {
			return fmt.Errorf("reading migration %s: %w", name, err)
		}
		if err := recordMigrationVersion(ctx, db, version, true); err != nil {
			return err
		}
		if _, err := db.ExecContext(ctx, string(contents)); err != nil {
			return fmt.Errorf("applying migration %s: %w", name, err)
		}
		if err := recordMigrationVersion(ctx, db, version, false); err != nil {
			return err
		}
		currentVersion = version
	}

	return nil
}

func currentMigrationVersion(ctx context.Context, db *sql.DB) (int, bool, error) {
	var version int
	var dirty bool
	err := db.QueryRowContext(ctx, `SELECT version, dirty FROM schema_migrations LIMIT 1`).Scan(&version, &dirty)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("reading schema migration version: %w", err)
	}
	return version, dirty, nil
}

func recordMigrationVersion(ctx context.Context, db *sql.DB, version int, dirty bool) error {
	if _, err := db.ExecContext(ctx, `DELETE FROM schema_migrations`); err != nil {
		return fmt.Errorf("clearing schema migration version: %w", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO schema_migrations (version, dirty) VALUES ($1, $2)`, version, dirty); err != nil {
		return fmt.Errorf("recording migration version %d: %w", version, err)
	}
	return nil
}

func migrationVersion(name string) (int, error) {
	prefix, _, ok := strings.Cut(name, "_")
	if !ok {
		return 0, fmt.Errorf("invalid migration filename %q", name)
	}
	version, err := strconv.Atoi(prefix)
	if err != nil {
		return 0, fmt.Errorf("invalid migration version in %q: %w", name, err)
	}
	return version, nil
}
