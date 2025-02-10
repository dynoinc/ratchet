# Stage 1: Build the Go application
FROM golang:1.23 AS builder

ENV GOTOOLCHAIN=auto
ENV CGO_ENABLED=0

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build ./cmd/ratchet

# Stage 2: Create the final image
FROM public.ecr.aws/lts/ubuntu:edge AS runner

# Update and install packages with specific versions for security
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
    ca-certificates \
    libc-bin=2.39-0ubuntu8.4 && \
    update-ca-certificates && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

# Create a non-root user
RUN useradd -r -u 10001 -g 0 appuser

# Copy the binary from builder
COPY --from=builder /app/ratchet .

# Set ownership of the application
RUN chown appuser:0 /ratchet && chmod +x /ratchet

# Use non-root user
USER 10001

ENTRYPOINT ["./ratchet"]
