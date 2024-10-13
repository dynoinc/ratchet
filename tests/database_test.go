package tests

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/rajatgoel/ratchet/internal"
)

func TestDBConnection(t *testing.T) {
	ctx := context.Background()
	postgresContainer, err := postgres.Run(ctx, "postgres:latest", postgres.BasicWaitStrategies())
	require.NoError(t, err)

	err = internal.TestDBConnection(postgresContainer.MustConnectionString(ctx))
	require.NoErrorf(t, err, postgresContainer.MustConnectionString(ctx))
}
