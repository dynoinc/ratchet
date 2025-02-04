# ratchet [![build](https://github.com/dynoinc/ratchet/actions/workflows/build.yml/badge.svg?branch=main)](https://github.com/dynoinc/ratchet/actions/workflows/build.yml)

AI bot to help reduce operational toil

## Lessons learned

* The idea to keep things simple by only using Postgres and Slack integration is working out.
  * Though Slack API is pretty badly documented and not consistent.
  
* Investing into building UI for visibility ended up wasting a lot of time. 
  * Even with AI tools, it is hard to get right for a backend engineer.
  * Even after you figure out HTML/CSS/JS, dealing with security concernsand deploying to production is a pain.
  * River and its UI is great.

* For database schema, instead of using individual columns for each attribute, using `attrs` column as jsonb is great.
  * SQLc support for custom types and automatically serialize/deserialize to jsonb is great.

* Given the focus of the bot is on AI, could have saved time by - 
  * Not focusing on non-AI features (like matching open/close incidents with alerts).
  * By not aiming for perfect data collection, when AI is good with imperfect data.

* For LLM, ollama is great for local development.

## Contributing

* To a slack workspace where you are admin, add an app using the manifest from `app-manifest.yaml`.
* Get access to a working Slack app/bot token and add it to `.env` (.gitignore'd) file as -

```
  RATCHET_SLACK_APP_TOKEN=xapp-...
  RATCHET_SLACK_BOT_TOKEN=xoxb-...
  RATCHET_CLASSIFIER_INCIDENT_CLASSIFICATION_BINARY=path/to/binary

```

* Just start the binary using `go run ./cmd/ratchet`. It will use docker to start Postgres instance if not already running.
