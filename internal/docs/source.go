package docs

import (
	"context"
	"fmt"
	"iter"
	"os"

	"github.com/go-playground/validator/v10"
	"gopkg.in/yaml.v3"
)

type Source struct {
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
	Deleted  bool
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
