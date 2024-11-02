# ratchet [![build](https://github.com/dynoinc/ratchet/actions/workflows/build.yml/badge.svg?branch=main)](https://github.com/dynoinc/ratchet/actions/workflows/build.yml)
AI bot to help reduce operational toil

## Code organization

* We are trying to keep things simple by only targeting one database, integration, etc.
* For backend, we use `postgres` managed by `sqlc`. All that code is in `internal/storage`.
* Think of slack integration (at `internal/slack`) as a client and bot (at `internal`) as server. 
  * Based on interactions that happens on slack, bot returns 0 or more actions to take. 
  * This way we can write tests for the bot without messing with slack.
  * Later, we should consider replaying Slack HTTP API requests/responses to test this integration.

## Database Schema

* The whole thing revolves around "Service" (to account for re-orgs)
* Service have alerts.
* Alerts have runbooks. All the past versions are kept in system, exactly one is active.
* Service has human asking for help. The whole slack thread is one instance of human interaction.
* Service has bot sending notifications about events related to it. Each notification is a separate instance.
* Alerts, humans and bot notifications come via one or more channels.
* Each channel is owned by a team. Owning team can change. Team can have multiple channels.
* Team names can be changed, teams can merge.
* Each human is either a member of a team or a customer asking for help.

## Contributing

* To a slack workspace where you are admin, add an app using the manifest from `app-manifest.yaml`.
* Get access to a working Slack app/bot token and add it to `.env` (.gitignore'd) file as -
```
  RATCHET_SLACK_APP_TOKEN=xapp-...
  RATCHET_SLACK_BOT_TOKEN=xoxb-...
```
* Just start the binary using `go run ./cmd/ratchet/main.go`. It depends on docker to start a postgres instance.
