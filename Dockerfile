# Stage 1: Build the Go application
FROM golang:1.23 AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -buildvcs=true -o ratchet ./cmd/ratchet/main.go

# Stage 2: Create the final image
FROM ubuntu:22.04

RUN apt update
RUN apt install -y ca-certificates
RUN update-ca-certificates

WORKDIR /
COPY --from=builder /app/ratchet .

ENTRYPOINT ["./ratchet"]
