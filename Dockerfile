# Stage 1: Build the Go application
FROM golang:1.23 AS builder

WORKDIR /app
COPY go.mod go.sum .
RUN go mod download

COPY . .
RUN go build ./cmd/ratchet

# Stage 2: Create the final image
FROM ubuntu:24.04

RUN apt update && apt install -y ca-certificates && update-ca-certificates

COPY --from=builder /app/ratchet .

ENTRYPOINT ["./ratchet"]
