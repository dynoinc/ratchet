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
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o ratchet ./cmd/ratchet/main.go

# Stage 2: Create the final image
FROM alpine:latest

# Install bash
RUN apk add --no-cache bash

# Set the working directory inside the container
WORKDIR /

# Copy the built binary from the builder stage
COPY --from=builder /app/ratchet .

# Set the entry point to the application
ENTRYPOINT ["./ratchet"]
