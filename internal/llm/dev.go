package llm

import (
	"context"
	"fmt"
	"log"
	"math"
	"net/http"
	"slices"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	"github.com/ollama/ollama/api"
)

const (
	containerName = "ratchet-ollama"
	ollamaImage   = "ollama/ollama"
	ollamaURL     = "http://localhost:11434/"
	modelName     = "mistral:latest"
)

func StartOllamaContainer(ctx context.Context, c LLMConfig) error {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return fmt.Errorf("failed to create Docker client: %v", err)
	}

	// Check if the container is already running
	existingContainerID, err := findRunningContainer(cli, ctx, containerName)
	if err != nil {
		return fmt.Errorf("failed to find running container: %v", err)
	}
	if existingContainerID != "" {
		return nil
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

	ollamaClient, err := api.ClientFromEnvironment()
	if err != nil {
		return fmt.Errorf("failed to create Ollama client: %v", err)
	}

	models, err := ollamaClient.List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list Ollama models: %v", err)
	}
	if !slices.ContainsFunc(models.Models, func(m api.ListModelResponse) bool { return m.Name == modelName }) {
		// pull the model
		if err := ollamaClient.Pull(ctx, &api.PullRequest{
			Model: modelName,
		}, func(p api.ProgressResponse) error {
			log.Printf("Downloading %s model: %v/%v\n", modelName, p.Completed, p.Total)
			return nil
		}); err != nil {
			return fmt.Errorf("failed to pull Ollama model: %v", err)
		}
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
