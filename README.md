# Ratchet 

[![build](https://github.com/dynoinc/ratchet/actions/workflows/build.yml/badge.svg?branch=main)](https://github.com/dynoinc/ratchet/actions/workflows/build.yml)

AI bot to help reduce operational toil

## How AI is used?

* To prepare a weekly report for a channel with suggestions of what to improve to reduce future support toil.
* To maintain runbooks for alerts. Runbooks include historical causes and debugging steps.
* To post relevant messages with runbooks when an alert is triggered.
* To provide a natural language interface to the bot.

## Built with

| Tool | Purpose |
|------|---------|
| [Go](https://go.dev/) | Main programming language for the application |
| [Slack](https://slack.com/) | Platform for bot interaction and message handling |
| [Ollama](https://ollama.com/) | Local LLM inference server for development |
| [PostgreSQL](https://www.postgresql.org/) | Primary database for storing messages, runbooks and embeddings |
| [SQLc](https://sqlc.dev/) | Type-safe SQL query generation from schema |
| [Riverqueue](http://riverqueue.com/) | Background job processing and scheduling |
| [pgvector](https://github.com/pgvector/pgvector) | Vector database for storing embeddings |
| [Docker](https://www.docker.com/) | Containerization and deployment |
| [Github Actions](https://github.com/features/actions) | CI/CD pipeline automation |

## Lessons learned

* PostgreSQL as database, queue and vector DB is working out.
* Slack as data source seems to be enough to derive value.
  * Though Slack API is poorly documented and inconsistent.
* Investing in building UI for visibility ended up wasting a lot of time. 
  * Even with AI tools, it is hard to get right for a backend engineer.
  * Even after you figure out HTML/CSS/JS, dealing with security concerns and deploying to production is a pain.
  * JSON API on the other hand is great. Just works and can post-process output with `jq` efficiently.
  * River and its UI is great though.
* For database schema, instead of using individual columns for each attribute, using `attrs` column as jsonb is great.
  * SQLc support for custom types and automatic serialization/deserialization to jsonb is great.
* Given the focus of the bot is on AI, could have saved time by:
  * Not focusing on non-AI features (like matching open/close incidents manually or building UI).
  * Not aiming for perfect data collection, when AI is good with imperfect data.
* On the AI front:
  * Ollama is great for local development.
  * qwen2.5:7b model is fast and good enough for local development.
  * Cursor IDE is great for writing code.
  * Using paid models like Claude Sonnet to improve your own prompt does wonders.
  * Giving LLM as much context as possible (like all historical messages instead of only new ones) helps.

## Things left to do for v0.1

- [ ] Finish the feedback loop of persisting user reactions to bot responses.
- [ ] Store detailed info about LLM calls to be able to fine-tune the model and create evals automatically.
- [ ] In channel report, point out alerts that are never acted on.
- [ ] In channel report, point out alerts that are open for too long.
- [ ] In channel report, fix user/bot name formatting.

## Contributing

* To a slack workspace where you are admin, add an app using the manifest from `app-manifest.yaml`.
* Get access to a working Slack app/bot token and add it to `.env` (.gitignore'd) file as -
```
  RATCHET_SLACK_APP_TOKEN=xapp-...
  RATCHET_SLACK_BOT_TOKEN=xoxb-...
  RATCHET_CLASSIFIER_INCIDENT_CLASSIFICATION_BINARY=path/to/binary
```
* Install docker (for postgres) and ollama (for local LLM access).
* Start the binary using 
```bash
  go run ./cmd/ratchet
```
