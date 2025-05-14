package storage

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"os"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"
	"github.com/docker/docker/errdefs"
	"github.com/docker/docker/pkg/jsonmessage"
	"github.com/docker/go-connections/nat"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	postgresImage = "pgvector/pgvector:pg12"
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
		return fmt.Errorf("creating Docker client: %w", err)
	}

	// Check if PostgreSQL image already exists
	_, err = cli.ImageInspect(ctx, postgresImage)
	if err != nil {
		if !errdefs.IsNotFound(err) {
			return fmt.Errorf("checking for Docker image: %w", err)
		}

		// Pull PostgreSQL image if not available
		reader, err := cli.ImagePull(ctx, postgresImage, image.PullOptions{})
		if err != nil {
			return fmt.Errorf("pulling Docker image: %w", err)
		}
		defer reader.Close()

		// Wait for image pull to complete and display progress
		termFd := os.Stdout.Fd()
		if err := jsonmessage.DisplayJSONMessagesStream(reader, os.Stdout, termFd, true, nil); err != nil {
			return fmt.Errorf("displaying pull progress: %w", err)
		}
	} else {
		slog.Info("PostgreSQL image already exists, skipping pull", "image", postgresImage)
	}

	// Define container configurations
	containerConfig := &container.Config{
		Image: postgresImage,
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
	var containerID string
	resp, err := cli.ContainerCreate(ctx, containerConfig, hostConfig, nil, nil, containerName)
	if err != nil {
		if !errdefs.IsConflict(err) {
			return fmt.Errorf("creating container: %w", err)
		}

		resp, err := cli.ContainerInspect(ctx, containerName)
		if err != nil {
			return fmt.Errorf("inspecting container: %w", err)
		}

		containerID = resp.ID
	} else {
		containerID = resp.ID
	}

	if err := cli.ContainerStart(ctx, containerID, container.StartOptions{}); err != nil {
		return fmt.Errorf("starting container: %w", err)
	}

	// Check readiness with exponential backoff
	if err := checkPostgresReady(ctx, c, 30); err != nil {
		return fmt.Errorf("PostgreSQL readiness check: %w", err)
	}

	// Return container ID and stop function
	return nil
}

// checkPostgresReady checks if PostgreSQL is ready by pinging it.
func checkPostgresReady(ctx context.Context, c DatabaseConfig, attempts int) error {
	pool, err := pgxpool.New(ctx, c.URL())
	if err != nil {
		return fmt.Errorf("creating connection pool: %w", err)
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
