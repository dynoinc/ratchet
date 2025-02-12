gen:
        go mod tidy
        go generate ./...
        go tool sqlc generate -f ./internal/storage/sqlc.yml

lint: gen
        go fmt ./...
        go vet ./...

test: lint
        go mod verify
        go build ./...
        go test -v -race ./...

reset:
        docker rm --force ratchet-db
        docker volume rm postgres_data
