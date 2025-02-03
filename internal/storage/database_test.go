package storage

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestDBSetup(t *testing.T) {
	ctx := context.Background()
	postgresContainer, err := postgres.Run(ctx, postgresImage, postgres.BasicWaitStrategies())
	require.NoError(t, err)
	t.Cleanup(func() { _ = postgresContainer.Stop(ctx, nil) })

	_, err = New(ctx, postgresContainer.MustConnectionString(ctx, "sslmode=disable"))
	require.NoError(t, err)
}
