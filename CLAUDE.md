# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Development Commands

Use the `just` command for all development tasks:

```bash
just gen    # Code generation (sqlc, goimports, go mod tidy)
just lint   # Linting (vet, staticcheck, govulncheck)  
just test   # Full test suite with race detection
just run    # Start development server
```

Database commands:
```bash
just pgshell  # PostgreSQL shell access
just reset    # Reset development database (removes container and volume)
```

## Architecture Overview

Ratchet is an AI-powered Slack bot for reducing operational toil. The architecture follows a modular plugin system:

- **Bot Coordinator** (`internal/bot.go`): Central message processing and database operations
- **Modules System** (`internal/modules/`): Pluggable handlers for different bot capabilities
- **Background Workers** (`internal/background/`): River queue-based async processing
- **Storage Layer** (`internal/storage/`): SQLC-generated type-safe database operations

### Key Components

**Core Services:**
- `internal/slack_integration/`: Slack API client and event handling
- `internal/llm/`: LLM abstraction (OpenAI/Ollama)
- `internal/storage/`: Database layer with PostgreSQL + pgvector

**Modules (Pluggable Handlers):**
- `modules/classifier/`: AI-powered incident classification
- `modules/channel_monitor/`: Channel activity monitoring and reporting
- `modules/commands/`: Bot command processing with MCP tools
- `modules/runbook/`: Automated runbook suggestions

**Background Workers:**
- `channel_onboard_worker/`: New channel setup and historical backfill
- `documentation_refresh_worker/`: Documentation sync and updates
- `modules_worker/`: Main message processing orchestrator

## Database & Schema

- **Tool**: SQLC for type-safe SQL generation
- **Migrations**: `/internal/storage/schema/migrations/`
- **Config**: `/internal/storage/sqlc.yml`
- **Features**: pgvector for embeddings, JSONB for flexible attributes

Run `just gen` after schema changes to regenerate Go code.

## Testing

- **Framework**: Standard Go testing with testify
- **Integration**: Uses testcontainers for PostgreSQL
- **Execution**: Race detection enabled (`go test -race`)
- **Coverage**: Run individual tests with `go test -v ./internal/path/to/package`

## Configuration

Development setup requires:
- `.env` file (gitignored) with Slack tokens
- Docker for PostgreSQL
- Ollama for local LLM (qwen2.5:7b model)

Key environment variables:
- `RATCHET_SLACK_APP_TOKEN` and `RATCHET_SLACK_BOT_TOKEN`
- `SENTRY_DSN` for error tracking
- `OTEL_EXPORTER_OTLP_ENDPOINT` for tracing

## Code Style Guidelines

- Avoid unnecessary comments - keep code concise
- Add unit tests for new functionality using mocks
- Use Conventional Commits format: `feat:`, `fix:`, etc.
- Follow OTEL attribute key conventions (use constants, not string literals)

## AI/LLM Integration

- **Production**: OpenAI API with function calling
- **Development**: Ollama with local models
- **Usage Tracking**: Stored in `llmusagev1` table
- **Context**: Provides full historical context to LLMs for better responses

## Development Workflow

1. Start PostgreSQL: `docker run --name ratchet-db -e POSTGRES_PASSWORD=postgres -d -p 5432:5432 postgres:16`
2. Configure `.env` with Slack tokens
3. Run `just run` to start development server
4. Use API endpoints for testing:
   - `POST /api/channels/{channel}/onboard` - Onboard channel
   - `GET /api/commands/generate?channel_id=X&ts=Y` - Process message