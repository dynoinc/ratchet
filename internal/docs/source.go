package docs

import (
	"context"
	"fmt"
	"iter"
	"os"
	"strings"

	"github.com/go-playground/validator/v10"
	"gopkg.in/yaml.v3"
)

type Source struct {
	Name   string        `yaml:"name"             validate:"required"`
	Type   string        `yaml:"type"             validate:"required,oneof=github"`
	GitHub *gitHubSource `yaml:"github,omitempty" validate:"required_if=Type github"`
}

func (s Source) URL() string {
	switch s.Type {
	case "github":
		return s.GitHub.URL()
	default:
		panic("unsupported source type")
	}
}

type Update struct {
	Revision string
	Path     string
	BlobSHA  string
}

func (s Source) ChangesSince(ctx context.Context, revision string) (iter.Seq[Update], string, func() error) {
	switch s.Type {
	case "github":
		return s.GitHub.changesSince(ctx, revision)
	default:
		panic("unsupported source type")
	}
}

func (s Source) Get(ctx context.Context, path, revision string) (string, error) {
	switch s.Type {
	case "github":
		return s.GitHub.get(ctx, path, revision)
	default:
		panic("unsupported source type")
	}
}

func (s Source) Suggest(ctx context.Context, path, revision, content string) (string, error) {
	switch s.Type {
	case "github":
		return s.GitHub.Suggest(ctx, path, revision, content)
	default:
		panic("unsupported source type")
	}
}

// DocumentURL represents the parts of a document URL
type DocumentURL struct {
	BaseURL  string // e.g., "https://github.com/owner/repo"
	Revision string // e.g., "main"
	Path     string // e.g., "docs/README.md"
}

// MakeURL constructs a full document URL from its parts
func MakeURL(baseURL, revision, path string) string {
	return fmt.Sprintf("%s/blob/%s/%s", baseURL, revision, path)
}

// ParseURL parses a document URL into its component parts
func ParseURL(url string) (*DocumentURL, error) {
	// Handle GitHub URLs: https://github.com/owner/repo/blob/revision/path
	if strings.Contains(url, "github.com") && strings.Contains(url, "/blob/") {
		parts := strings.Split(url, "/blob/")
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid GitHub URL format: %s", url)
		}

		baseURL := parts[0]
		pathPart := parts[1]
		pathParts := strings.SplitN(pathPart, "/", 2)
		if len(pathParts) != 2 {
			return nil, fmt.Errorf("invalid GitHub URL path format: %s", url)
		}

		return &DocumentURL{
			BaseURL:  baseURL,
			Revision: pathParts[0],
			Path:     pathParts[1],
		}, nil
	}

	return nil, fmt.Errorf("unsupported URL format: %s", url)
}

type Config struct {
	Sources []Source `yaml:"sources" validate:"required,dive"`
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse YAML: %w", err)
	}

	validate := validator.New()
	if err := validate.Struct(cfg); err != nil {
		return nil, fmt.Errorf("validation error: %w", err)
	}

	return &cfg, nil
}
