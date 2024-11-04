package storage

import (
	"context"
	"fmt"
	"log"
	"math"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	PostgresImage = "postgres:12.19"
)

// StartPostgresContainer starts a PostgreSQL container with persistent storage
// and checks for readiness with a "SELECT 1" query using exponential backoff.
// Returns the container ID and a function to stop the container.
func StartPostgresContainer(ctx context.Context, c DatabaseConfig) error {
	// Set up Docker client
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return fmt.Errorf("failed to create Docker client: %v", err)
	}

	// Check if the container is already running
	containerName := "ratchet-db"
	existingContainerID, err := findRunningContainer(cli, ctx, containerName)
	if err != nil {
		return fmt.Errorf("failed to find running container: %v", err)
	}
	if existingContainerID != "" {
		return nil
	}

	// Pull PostgreSQL image if not available
	if _, err = cli.ImagePull(ctx, PostgresImage, image.PullOptions{}); err != nil {
		return fmt.Errorf("failed to pull Docker image: %v", err)
	}

	// Define container configurations
	containerConfig := &container.Config{
		Image: PostgresImage,
		Env: []string{
			"POSTGRES_USER=" + c.DatabaseUser,
			"POSTGRES_PASSWORD=" + c.DatabasePass,
			"POSTGRES_DB=" + c.DatabaseName,
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
		return fmt.Errorf("failed to create container: %v", err)
	}

	if err := cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return fmt.Errorf("failed to start container: %v", err)
	}

	// Check readiness with exponential backoff
	if err := checkPostgresReady(ctx, c); err != nil {
		return fmt.Errorf("PostgreSQL readiness check failed: %v", err)
	}

	// Return container ID and stop function
	return nil
}

// findRunningContainer checks if a container with the given name is already running and returns its ID.
func findRunningContainer(cli *client.Client, ctx context.Context, containerName string) (string, error) {
	containers, err := cli.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return "", fmt.Errorf("failed to list containers: %v", err)
	}
	for _, c := range containers {
		for _, name := range c.Names {
			if name == "/"+containerName && c.State == "running" {
				return c.ID, nil
			}
		}
	}
	return "", nil
}

// checkPostgresReady checks if PostgreSQL is ready by pinging it.
func checkPostgresReady(ctx context.Context, c DatabaseConfig) error {
	pool, err := pgxpool.New(ctx, c.URL())
	if err != nil {
		return fmt.Errorf("failed to create connection pool: %v", err)
	}
	defer pool.Close()

	var backoff time.Duration
	for i := 0; i < 10; i++ {
		err = pool.Ping(ctx)
		if err == nil {
			return nil
		}

		backoff = time.Duration(math.Pow(2, float64(i))) * 100 * time.Millisecond
		log.Printf("PostgreSQL is not ready, retrying in %v: %v", backoff, err)
		time.Sleep(backoff)
	}

	return fmt.Errorf("PostgreSQL is not ready after multiple attempts")
}
