# ratchet [![build](https://github.com/rajatgoel/ratchet/actions/workflows/build.yml/badge.svg?branch=main)](https://github.com/rajatgoel/ratchet/actions/workflows/build.yml)
AI bot to help reduce operational toil

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

* To an slack workspace where you are admin, add an app using the manifest from `app-manifest.yaml`.
* Get access to a working Slack app/bot token and add it to `.env` (.gitignore'd) file as -
```
  RATCHET_SLACK_APP_TOKEN=xapp-...
  RATCHET_SLACK_BOT_TOKEN=xoxb-...
```
* Use `docker compose up --remove-orphans --watch --attach app` to start the stack locally.
  * Access `ratchet` UI at http://localhost:5001.
  * Passing `--attach app` will make docker-compose only show `app` service logs on terminal.
  * Passing `--watch` makes docker-compare sync+restart bot if new binary is available at `bin/ratchet`.
    * To automatically re-compile on update, use `fswatch go.sum internal/ cmd/ | GOOS=linux xargs -n1 -I{} go build -o bin/ratchet ./cmd/ratchet/main.go`
  * Or you can just use `itermocil`.
* Access `pgadmin` UI at http://localhost:8080.
  * Add a server with host `db` and password `mypass`.
