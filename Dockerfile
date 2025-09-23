# Stage 1: Build the Go application
FROM golang:1.24 AS builder

ENV GOTOOLCHAIN=auto
ENV CGO_ENABLED=0

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -trimpath -ldflags="-s -w" -o ratchet ./cmd/ratchet

# Stage 2: Create the final image
FROM public.ecr.aws/lts/ubuntu:24.04_stable AS runner

RUN apt-get update && \
    apt-get install -y --no-install-recommends unattended-upgrades && \
    unattended-upgrade --debug && \
    apt-get install -y --no-install-recommends --allow-downgrades ca-certificates && \
    update-ca-certificates && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

COPY --from=builder /app/ratchet .
ENTRYPOINT ["./ratchet"]
