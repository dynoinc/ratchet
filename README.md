# ratchet [![build](https://github.com/rajatgoel/ratchet/actions/workflows/build.yml/badge.svg?branch=main)](https://github.com/rajatgoel/ratchet/actions/workflows/build.yml)
AI bot to help reduce operational toil

## Contributing

* Get access to a working Slack app/bot token and add it to `.env` (.gitignore'd) file as -
```
  RATCHET_SLACK_APP_TOKEN=xapp-...
  RATCHET_SLACK_BOT_TOKEN=xoxb-...
```
* Use `docker compose up --remove-orphans --attach app` to start the stack locally.
  * Access `ratchet` UI at http://localhost:5001.
  * Passing `--attach app` will make docker-compose only show `app` service logs on terminal.
  * If you want to automatically re-build bot and changes, enable watch inside it by pressing `w` once it starts.
* Access `pgadmin` at https://localhost:8080. 
  * Login using username `postgres@admin.com` and password `mypass`. 
  * Add a server with host `db` and password `mypass`.
