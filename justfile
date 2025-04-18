gen:
        go mod tidy
        go generate ./...
        go tool sqlc generate -f ./internal/storage/sqlc.yml

lint: gen
        go fmt ./...
        go vet ./...
        go tool staticcheck ./...
        go tool govulncheck

test: lint
        go mod verify
        go build ./...
        go test -v -race ./...

run: gen
        go run ./cmd/ratchet

pgshell:
        podman exec -it ratchet-db psql -U postgres

reset:
        podman rm --force ratchet-db
        podman volume rm postgres_data
