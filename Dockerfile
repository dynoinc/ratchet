# Stage 1: Build the Go application
FROM golang:1.23-alpine AS builder

# Set the working directory inside the container
WORKDIR /app

# Copy go.mod and go.sum to the container
COPY go.mod go.sum ./

# Download Go modules
RUN go mod download

# Copy the rest of the application source code
COPY . .

# Build the application
RUN go build -o ratchet ./cmd/ratchet/main.go

# Stage 2: Create the final image
FROM ubuntu:22.04

RUN apt update
RUN apt install -y ca-certificates
RUN update-ca-certificates

# Set the working directory inside the container
WORKDIR /app

# Copy the built binary from the builder stage
COPY --from=builder /app/ratchet .

# Set the entry point to the application
ENTRYPOINT ["./ratchet"]
