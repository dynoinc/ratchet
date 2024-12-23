gen:
        go mod tidy
        sqlc generate -f ./internal/storage/sqlc.yml

lint: gen
        go fmt ./...
        go vet ./...

test: lint
        go test ./...
