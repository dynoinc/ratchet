package internal

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"log"
	"net/url"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rajatgoel/ratchet/internal/schema"
)

type DatabaseConfig struct {
	DatabaseHost       string `split_words:"true"`
	DatabasePort       int    `split_words:"true"`
	DatabaseUser       string `split_words:"true"`
	DatabasePass       string `split_words:"true"`
	DatabaseName       string `split_words:"true"`
	DatabaseDisableTLS bool   `split_words:"true" default:"false"`
}

//go:embed schema/migrations/*.sql
var migrationFiles embed.FS

func NewDBConnection(ctx context.Context, c DatabaseConfig) (*schema.Queries, error) {
	dbURL := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(c.DatabaseUser, c.DatabasePass),
		Host:   fmt.Sprintf("%s:%d", c.DatabaseHost, c.DatabasePort),
		Path:   c.DatabaseName,
	}
	if c.DatabaseDisableTLS {
		dbURL.RawQuery = "sslmode=disable"
	}

	return NewDBConnectionWithURL(ctx, dbURL.String())
}

func NewDBConnectionWithURL(ctx context.Context, dbURL string) (*schema.Queries, error) {
	d, err := iofs.New(migrationFiles, "schema/migrations")
	if err != nil {
		return nil, fmt.Errorf("unable to load migrations: %w", err)
	}

	m, err := migrate.NewWithSourceInstance("iofs", d, dbURL)
	if err != nil {
		return nil, fmt.Errorf("unable to create driver: %w", err)
	}

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return nil, fmt.Errorf("unable to apply migrations: %w", err)
	}

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		return nil, fmt.Errorf("unable to connect to database: %w", err)
	}

	log.Println("Migrations applied successfully!")
	return schema.New(pool), nil
}
