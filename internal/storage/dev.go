package storage

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"os/exec"
	"strings"
	"time"

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

	// Check if PostgreSQL image already exists
	if err := runDocker(ctx, "image", "inspect", postgresImage); err != nil {
		if err := runDocker(ctx, "pull", postgresImage); err != nil {
			return fmt.Errorf("pulling Docker image: %w", err)
		}
	} else {
		slog.Info("PostgreSQL image already exists, skipping pull", "image", postgresImage)
	}

	// Create and start the PostgreSQL container
	if err := runDocker(ctx, "container", "inspect", containerName); err != nil {
		if err := runDocker(ctx,
			"create",
			"--name", containerName,
			"--env", "POSTGRES_USER="+c.User,
			"--env", "POSTGRES_PASSWORD="+c.Pass,
			"--env", "POSTGRES_DB="+c.Name,
			"--publish", "127.0.0.1:5432:5432",
			"--mount", "type=volume,src=postgres_data,dst=/var/lib/postgresql/data",
			postgresImage,
		); err != nil {
			return fmt.Errorf("creating container: %w", err)
		}
	}

	if err := runDocker(ctx, "start", containerName); err != nil {
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

func runDocker(ctx context.Context, args ...string) error {
	_, err := dockerOutput(ctx, args...)
	return err
}

func dockerOutput(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}
