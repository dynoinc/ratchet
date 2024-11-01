package tests

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/rajatgoel/ratchet/internal/storage"
)

func SetupStorage(t *testing.T) *pgxpool.Pool {
	t.Helper()

	ctx := context.Background()
	postgresContainer, err := postgres.Run(ctx, "postgres:latest", postgres.BasicWaitStrategies())
	require.NoError(t, err)
	t.Cleanup(func() { _ = postgresContainer.Stop(ctx, nil) })

	db, err := storage.NewDBConnectionWithURL(ctx, postgresContainer.MustConnectionString(ctx, "sslmode=disable"))
	require.NoError(t, err)
	return db
}

func TestDBConnection(t *testing.T) {
	_ = SetupStorage(t)
}
