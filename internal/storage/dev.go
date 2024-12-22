package storage

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"
	"github.com/docker/docker/errdefs"
	"github.com/docker/go-connections/nat"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	PostgresImage = "postgres:12.19"
	containerName = "ratchet-db"
)

// StartPostgresContainer starts a PostgreSQL container with persistent storage
// and checks for readiness with a PING using exponential backoff.
func StartPostgresContainer(ctx context.Context, c DatabaseConfig) error {
	// If postgres is already running, return
	if checkPostgresReady(ctx, c, 1) == nil {
		return nil
	}

	// Set up Docker client
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return fmt.Errorf("failed to create Docker client: %v", err)
	}

	// Pull PostgreSQL image if not available
	if _, err = cli.ImagePull(ctx, PostgresImage, image.PullOptions{}); err != nil {
		return fmt.Errorf("failed to pull Docker image: %v", err)
	}

	// Define container configurations
	containerConfig := &container.Config{
		Image: PostgresImage,
		Env: []string{
			"POSTGRES_USER=" + c.User,
			"POSTGRES_PASSWORD=" + c.Pass,
			"POSTGRES_DB=" + c.Name,
		},
		ExposedPorts: nat.PortSet{
			"5432/tcp": struct{}{},
		},
	}

	// Define host configuration with port mapping and volume mount for persistence
	hostConfig := &container.HostConfig{
		PortBindings: nat.PortMap{
			"5432/tcp": []nat.PortBinding{
				{
					HostIP:   "127.0.0.1",
					HostPort: "5432",
				},
			},
		},
		Mounts: []mount.Mount{
			{
				Type:   mount.TypeVolume,
				Source: "postgres_data",
				Target: "/var/lib/postgresql/data",
			},
		},
	}

	// Create and start the PostgreSQL container
	resp, err := cli.ContainerCreate(ctx, containerConfig, hostConfig, nil, nil, containerName)
	if err != nil {
		if !errdefs.IsConflict(err) {
			return fmt.Errorf("failed to create container: %v", err)
		}
	}

	if err := cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return fmt.Errorf("failed to start container: %v", err)
	}

	// Check readiness with exponential backoff
	if err := checkPostgresReady(ctx, c, 30); err != nil {
		return fmt.Errorf("PostgreSQL readiness check failed: %v", err)
	}

	// Return container ID and stop function
	return nil
}

// checkPostgresReady checks if PostgreSQL is ready by pinging it.
func checkPostgresReady(ctx context.Context, c DatabaseConfig, attempts int) error {
	pool, err := pgxpool.New(ctx, c.URL())
	if err != nil {
		return fmt.Errorf("failed to create connection pool: %v", err)
	}
	defer pool.Close()

	var backoff time.Duration
	for i := 0; i < attempts; i++ {
		err = pool.Ping(ctx)
		if err == nil {
			return nil
		}

		backoff = time.Duration(math.Pow(2, float64(i))) * 100 * time.Millisecond
		slog.Info("PostgreSQL is not ready, retrying", "backoff", backoff, "error", err)
		time.Sleep(backoff)
	}

	return fmt.Errorf("PostgreSQL is not ready after multiple attempts")
}
