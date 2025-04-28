package storage

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"strconv"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	pgxvector "github.com/pgvector/pgvector-go/pgx"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
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
	d, err := iofs.New(migrationFiles, "schema/migrations")
	if err != nil {
		return nil, fmt.Errorf("loading migrations: %w", err)
	}

	m, err := migrate.NewWithSourceInstance("iofs", d, dbURL)
	if err != nil {
		return nil, fmt.Errorf("creating driver: %w", err)
	}

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return nil, fmt.Errorf("applying migrations: %w", err)
	}

	config.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		return pgxvector.RegisterTypes(ctx, conn)
	}

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("connecting to database: %w", err)
	}

	slog.Info("Migrations applied successfully!")
	return pool, nil
}
