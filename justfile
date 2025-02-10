gen:
        go mod tidy
        sqlc generate -f ./internal/storage/sqlc.yml

lint: gen
        go fmt ./...
        go vet ./...

test: lint
        go build ./...
        go test -v -race ./...

reset:
        docker rm --force ratchet-db
        docker volume rm postgres_data
