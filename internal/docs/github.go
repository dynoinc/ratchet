package docs

import (
	"context"
	"fmt"
	"iter"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/go-github/v53/github"

	"github.com/dynoinc/ratchet/internal/github_integration"
)

type gitHubSource struct {
	AppID          int64  `yaml:"app_id"        validate:"required,gt=0"`
	InstallationID int64  `yaml:"installation_id"  validate:"required,gt=0"`
	PrivateKeyPath string `yaml:"private_key_path" validate:"required"`

	GitHubURL string `yaml:"github_url"       validate:"required,url"`
	Owner     string `yaml:"owner"            validate:"required"`
	Repo      string `yaml:"repo"             validate:"required"`
	Path      string `yaml:"path"             validate:"required"`
}

func (gs *gitHubSource) URL() string {
	return fmt.Sprintf("%s/%s/%s", gs.GitHubURL, gs.Owner, gs.Repo)
}

func (gs *gitHubSource) ChangesSince(ctx context.Context, revision string) (iter.Seq[Update], string, func() error) {
	// Get GitHub client
	start := time.Now()
	client, err := github_integration.For(gs.AppID, gs.InstallationID, gs.PrivateKeyPath)
	slog.Debug("GitHub client initialization", "duration", time.Since(start))
	if err != nil {
		return nil, revision, func() error {
			return fmt.Errorf("creating GitHub client: %w", err)
		}
	}

	// Get the latest commit SHA for the repository
	start = time.Now()
	repo, _, err := client.Repositories.Get(ctx, gs.Owner, gs.Repo)
	slog.Debug("GitHub API: get repository", "owner", gs.Owner, "repo", gs.Repo, "duration", time.Since(start))
	if err != nil {
		return nil, revision, func() error {
			return fmt.Errorf("getting repository: %w", err)
		}
	}

	defaultBranch := repo.GetDefaultBranch()
	start = time.Now()
	ref, _, err := client.Git.GetRef(ctx, gs.Owner, gs.Repo, "refs/heads/"+defaultBranch)
	slog.Debug("GitHub API: get reference", "ref", "refs/heads/"+defaultBranch, "duration", time.Since(start))
	if err != nil {
		return nil, revision, func() error {
			return fmt.Errorf("getting reference for default branch: %w", err)
		}
	}
	newRevision := ref.GetObject().GetSHA()

	// If no revision is provided, get the current snapshot
	if revision == "" {
		var capturedError error
		return iter.Seq[Update](func(yield func(Update) bool) {
				// Recursively walk through the directory structure
				var walkDir func(path string) bool
				walkDir = func(path string) bool {
					start := time.Now()
					_, dirContent, _, err := client.Repositories.GetContents(
						ctx,
						gs.Owner,
						gs.Repo,
						path,
						&github.RepositoryContentGetOptions{Ref: newRevision},
					)
					slog.Debug("GitHub API: get directory contents", "path", path, "duration", time.Since(start))
					if err != nil {
						capturedError = fmt.Errorf("getting contents of %s: %w", path, err)
						return false
					}

					for _, content := range dirContent {
						if content.GetType() == "dir" {
							if !walkDir(content.GetPath()) {
								return false
							}
						} else if content.GetType() == "file" {
							// Only process .md or .txt files
							filePath := content.GetPath()
							ext := strings.ToLower(filepath.Ext(filePath))
							if ext != ".md" && ext != ".txt" {
								continue
							}

							if !yield(Update{
								Path:     filePath,
								Revision: newRevision,
							}) {
								return false
							}
						}
					}
					return true
				}

				// Start the recursive walk from the root path
				walkDir(gs.Path)
			}), newRevision, func() error {
				return capturedError
			}
	}

	// If revision is provided, get changes since that revision
	var capturedError error
	return iter.Seq[Update](func(yield func(Update) bool) {
			// Get the comparison between revisions
			opts := &github.ListOptions{PerPage: 100}

			for {
				start := time.Now()
				comparison, resp, err := client.Repositories.CompareCommits(
					ctx,
					gs.Owner,
					gs.Repo,
					revision,
					newRevision,
					opts,
				)
				slog.Debug("GitHub API: compare commits", "from", revision, "to", newRevision, "page", opts.Page, "duration", time.Since(start))
				if err != nil {
					capturedError = fmt.Errorf("comparing commits: %w", err)
					return
				}

				// Process each file in this page of the comparison
				for _, file := range comparison.Files {
					filePath := file.GetFilename()
					slog.Debug("GitHub API: file", "path", filePath, "status", file.GetStatus(), "prefix", gs.Path)

					// Skip files not in our path
					if !strings.HasPrefix(filePath, gs.Path) {
						continue
					}

					// Only process .md or .txt files
					ext := strings.ToLower(filepath.Ext(filePath))
					if ext != ".md" && ext != ".txt" {
						continue
					}

					switch file.GetStatus() {
					case "added", "modified":
						if !yield(Update{
							Path:     filePath,
							Revision: newRevision,
						}) {
							return
						}

					case "removed":
						if !yield(Update{
							Path:     filePath,
							Revision: newRevision,
							Deleted:  true,
						}) {
							return
						}
					}
				}

				// Check if there are more pages
				if resp.NextPage == 0 {
					break
				}
				opts.Page = resp.NextPage
			}
		}), newRevision, func() error {
			return capturedError
		}
}

func (gs *gitHubSource) Get(ctx context.Context, path, revision string) (string, error) {
	client, err := github_integration.For(gs.AppID, gs.InstallationID, gs.PrivateKeyPath)
	if err != nil {
		return "", fmt.Errorf("creating GitHub client: %w", err)
	}

	fileContent, _, _, err := client.Repositories.GetContents(
		ctx,
		gs.Owner,
		gs.Repo,
		path,
		&github.RepositoryContentGetOptions{Ref: revision},
	)
	if err != nil {
		return "", fmt.Errorf("getting file %s: %w", path, err)
	}

	return fileContent.GetContent()
}

func (gs *gitHubSource) Suggest(ctx context.Context, path, revision, content string) (string, error) {
	client, err := github_integration.For(gs.AppID, gs.InstallationID, gs.PrivateKeyPath)
	if err != nil {
		return "", fmt.Errorf("creating GitHub client: %w", err)
	}

	// Get the current file to obtain its SHA
	fileContent, _, _, err := client.Repositories.GetContents(
		ctx,
		gs.Owner,
		gs.Repo,
		path,
		&github.RepositoryContentGetOptions{Ref: revision},
	)
	if err != nil {
		return "", fmt.Errorf("getting file %s: %w", path, err)
	}

	// Create a unique branch name for the PR
	timestamp := time.Now().Unix()
	branchName := fmt.Sprintf("docs-update-%d", timestamp)

	// Create a new branch
	newRef := &github.Reference{
		Ref:    github.String("refs/heads/" + branchName),
		Object: &github.GitObject{SHA: &revision},
	}
	_, _, err = client.Git.CreateRef(ctx, gs.Owner, gs.Repo, newRef)
	if err != nil {
		return "", fmt.Errorf("creating branch: %w", err)
	}

	// Update file in the new branch
	opts := &github.RepositoryContentFileOptions{
		Message: github.String("Update documentation"),
		Content: []byte(content),
		SHA:     fileContent.SHA,
		Branch:  github.String(branchName),
	}
	_, _, err = client.Repositories.UpdateFile(ctx, gs.Owner, gs.Repo, path, opts)
	if err != nil {
		return "", fmt.Errorf("updating file: %w", err)
	}

	// Get repository info to determine the default branch
	repo, _, err := client.Repositories.Get(ctx, gs.Owner, gs.Repo)
	if err != nil {
		return "", fmt.Errorf("getting repository info: %w", err)
	}
	defaultBranch := repo.GetDefaultBranch()

	// Create a pull request
	prTitle := fmt.Sprintf("Documentation update for %s", filepath.Base(path))
	prBody := "This PR contains documentation updates suggested by Ratchet."
	pr := &github.NewPullRequest{
		Title:               github.String(prTitle),
		Head:                github.String(branchName),
		Base:                github.String(defaultBranch),
		Body:                github.String(prBody),
		MaintainerCanModify: github.Bool(true),
		Draft:               github.Bool(true),
	}

	newPR, _, err := client.PullRequests.Create(ctx, gs.Owner, gs.Repo, pr)
	if err != nil {
		return "", fmt.Errorf("creating pull request: %w", err)
	}

	// Log the PR creation
	slog.InfoContext(ctx, "Created documentation update PR",
		"url", newPR.GetHTMLURL(),
		"repo", fmt.Sprintf("%s/%s", gs.Owner, gs.Repo),
		"path", path)

	// Return the PR URL
	return newPR.GetHTMLURL(), nil
}
