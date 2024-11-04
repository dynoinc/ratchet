package storage

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"log"
	"net"
	"net/url"
	"strconv"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
)

//go:embed schema/migrations/*.sql
var migrationFiles embed.FS

type DatabaseConfig struct {
	DatabaseHost       string `split_words:"true" default:"localhost"`
	DatabasePort       int    `split_words:"true" default:"5432"`
	DatabaseUser       string `split_words:"true" default:"postgres"`
	DatabasePass       string `split_words:"true" default:"password"`
	DatabaseName       string `split_words:"true" default:"postgres"`
	DatabaseDisableTLS bool   `split_words:"true" default:"true"`
}

func (c DatabaseConfig) URL() string {
	dbURL := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(c.DatabaseUser, c.DatabasePass),
		Host:   net.JoinHostPort(c.DatabaseHost, strconv.Itoa(c.DatabasePort)),
		Path:   c.DatabaseName,
	}
	if c.DatabaseDisableTLS {
		dbURL.RawQuery = "sslmode=disable"
	}

	return dbURL.String()
}

func New(ctx context.Context, dbURL string) (*pgxpool.Pool, error) {
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

	rm, err := rivermigrate.New(riverpgxv5.New(pool), nil)
	if err != nil {
		return nil, fmt.Errorf("unable to create river migrate: %w", err)
	}

	_, err = rm.Migrate(ctx, rivermigrate.DirectionUp, nil)
	if err != nil {
		return nil, fmt.Errorf("unable to apply river migrations: %w", err)
	}

	log.Println("Migrations applied successfully!")
	return pool, nil
}
