default: test

gen:
        go mod tidy
        go generate ./...
        go tool sqlc generate -f ./internal/storage/sqlc.yml

lint: gen
        go tool goimports -local=github.com/dynoinc/ratchet -w .
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
        docker exec -it ratchet-db psql -U postgres -d ratchet

reset:
        docker rm --force ratchet-db
        docker volume rm postgres_data
