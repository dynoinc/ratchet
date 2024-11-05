package llm

import (
	"context"
	"fmt"
	"log"
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
	if checkHealth(ollamaURL) == nil {
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

	if err := checkHealth(ollamaURL); err != nil {
		return fmt.Errorf("failed to check Ollama health: %v", err)
	}

	return nil
}

func checkHealth(url string) error {
	var backoff time.Duration
	for retries := 0; retries < 10; retries++ {
		resp, err := http.Get(url)
		if resp != nil {
			defer resp.Body.Close()
		}
		if err == nil && resp.StatusCode == http.StatusOK {
			log.Println("Ollama health check passed.")
			return nil
		}

		backoff = time.Duration(math.Pow(2, float64(retries))) * 100 * time.Millisecond
		log.Printf("Ollama is not ready, retrying in %v: %v", backoff, err)
		time.Sleep(backoff)
	}

	return fmt.Errorf("ollama health check failed after multiple attempts")
}
