# Stage 1: Build the Go application
FROM golang:latest AS builder

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
    if dpkg --compare-versions "$(dpkg-query -f '${Version}' -W gpgv 2>/dev/null || echo '0')" ge "2.4.4-2ubuntu17.2"; then \
        echo "gpgv is already at or above required version. Failing build." && exit 1; \
    fi && \
    apt-get install -y --no-install-recommends \
        gpgv=2.4.4-2ubuntu17.2 \
        ca-certificates && \
    update-ca-certificates && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

COPY --from=builder /app/ratchet .
ENTRYPOINT ["./ratchet"]
