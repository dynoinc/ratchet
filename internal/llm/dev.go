package llm

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
)

const (
	containerName = "ratchet-ollama"
	ollamaImage   = "ollama/ollama"
	ollamaURL     = "http://localhost:11434/"
)

func StartOllamaContainer(ctx context.Context) error {
	// If local ollama is running, just use that.
	if checkHealth(ctx, ollamaURL, 1) == nil {
		return nil
	}

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return fmt.Errorf("failed to create Docker client: %v", err)
	}

	out, err := cli.ImagePull(ctx, ollamaImage, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("failed to pull Docker image: %v", err)
	}
	defer out.Close()

	// Create and start container
	resp, err := cli.ContainerCreate(ctx, &container.Config{
		Image: ollamaImage,
		ExposedPorts: nat.PortSet{
			"11434/tcp": struct{}{},
		},
	}, &container.HostConfig{
		PortBindings: nat.PortMap{
			"11434/tcp": []nat.PortBinding{
				{HostIP: "127.0.0.1", HostPort: "11434"},
			},
		},
	}, &network.NetworkingConfig{}, nil, containerName)
	if err != nil {
		return fmt.Errorf("failed to create container: %v", err)
	}

	if err := cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return fmt.Errorf("failed to start container: %v", err)
	}

	if err := checkHealth(ctx, ollamaURL, 30); err != nil {
		return fmt.Errorf("failed to check Ollama health: %v", err)
	}

	return nil
}

func checkHealth(ctx context.Context, url string, attempts int) error {
	var backoff time.Duration
	for retries := 0; retries < attempts; retries++ {
		resp, err := http.Get(url)
		if resp != nil {
			defer func() { _ = resp.Body.Close() }()
		}

		if err == nil && resp.StatusCode == http.StatusOK {
			slog.InfoContext(ctx, "Ollama health check passed")
			return nil
		}

		backoff = time.Duration(math.Pow(2, float64(retries))) * 100 * time.Millisecond
		slog.InfoContext(ctx, "Ollama is not ready, retrying", "backoff", backoff, "error", err)
		time.Sleep(backoff)
	}

	return fmt.Errorf("ollama health check failed after multiple attempts")
}
