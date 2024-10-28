# ratchet [![build](https://github.com/rajatgoel/ratchet/actions/workflows/build.yml/badge.svg?branch=main)](https://github.com/rajatgoel/ratchet/actions/workflows/build.yml)
AI bot to help reduce operational toil

## Database Schema
* `slack_channels`
  * [PK] `channel_id`
  * `team_name`
  * `enabled` (bool)

* `slack_activities` (links to `slack_channels`)
  * [PK] `channel_id`
  * [PK] `activity_slack_ts` (timestamp of the root message)
  * `activity_type` (enum: `alert`, `human`, `bot`)

* `slack_messages` (ties to `slack_activities`)
  * [PK] `channel_id`
  * [PK] `activity_slack_ts`
  * [PK] `message_channel_id` (allows us to link messages in other channels to the same activity)
  * [PK] `slack_ts`
  * `user_id`
  * `user_type` (enum: `human`, `bot`)
  * `text`
  * `reactions`

* `alerts` (links to `slack_activities` of type `alert`. sparse index on unresolved alerts)
  * [PK] `channel_id`
  * [PK] `activity_slack_ts`
  * `triggered_ts` (timestamp when alert was triggered)
  * `resolved_ts` (timestamp when alert was resolved, nil if unresolved)
  * `alert_name`
  * `service`
  * `severity` (enum: `low`, `high`)
  * `actionable` (bool)
  * `root_cause_category` (enum: `bug`, `dependency failure`, `misconfigured`, `other`)
  * `root_cause` (free text)

* `alerts_runbook` (links to `slack_channels`. sparse index on active runbook)
  * [PK] `channel_id`
  * [PK] `alert_name`
  * [PK] `service`
  * [PK] `created_ts`
  * `runbook`
  * `active` (bool, at most one active runbook per alert)
  * `source` (jsonb, ai model that generated the runbook or other info about how runbook was generated)

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
* Access `pgadmin` UI at http://localhost:8080.
  * Add a server with host `db` and password `mypass`.
