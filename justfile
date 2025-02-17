gen:
        go mod tidy
        go generate ./...
        go tool sqlc generate -f ./internal/storage/sqlc.yml

lint: gen
        go fmt ./...
        go vet ./...
        go tool staticcheck ./...

test: lint
        go mod verify
        go build ./...
        go test -v -race ./...

run: gen
        go run ./cmd/ratchet

reset:
        docker rm --force ratchet-db
        docker volume rm postgres_data
