# ratchet [![build](https://github.com/rajatgoel/ratchet/actions/workflows/build.yml/badge.svg?branch=main)](https://github.com/rajatgoel/ratchet/actions/workflows/build.yml)
AI bot to help reduce operational toil

## Contributing

* Get access to a working Slack app/bot token and add it to `.env` file as RRATCHET_SLACK_APP_TOKEN/RATCHET_SLACK_BOT_TOKEN respectively.
* Use `docker compose up --remove-orphans` to start the stack locally. Enable watch inside it by pressing `w` once it starts.
* Access `pgadmin` at https://localhost:8080. Login using username `postgres@admin.com` and password `mypass`. Add a server with host `db` and password `mypass`.

