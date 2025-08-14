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
    apt-get install -y --no-install-recommends --allow-downgrades \
        --only-upgrade libudev1=255.4-1ubuntu8.8 \
        --only-upgrade libsystemd0=255.4-1ubuntu8.8 \
        --only-upgrade libpam0g=1.5.3-5ubuntu5.4 \
        --only-upgrade libpam-runtime=1.5.3-5ubuntu5.4 \
        --only-upgrade libpam-modules-bin=1.5.3-5ubuntu5.4 \
        --only-upgrade libpam-modules=1.5.3-5ubuntu5.4 && \
    apt-get install -y --no-install-recommends --allow-downgrades ca-certificates && \
    update-ca-certificates && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

COPY --from=builder /app/ratchet .
ENTRYPOINT ["./ratchet"]
