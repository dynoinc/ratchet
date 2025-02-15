# Stage 1: Build the Go application
FROM golang:1.24 AS builder

ENV GOTOOLCHAIN=auto
ENV CGO_ENABLED=0

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build ./cmd/ratchet

# Stage 2: Create the final image
FROM public.ecr.aws/lts/ubuntu:edge AS runner

RUN apt-get update && \
    if dpkg --compare-versions "$(dpkg-query -f '${Version}' -W libc-bin)" lt "2.39-0ubuntu8.4"; then \
        apt-get install -y --no-install-recommends libc-bin=2.39-0ubuntu8.4; \
    fi && \
    apt-get install -y --no-install-recommends ca-certificates && \
    update-ca-certificates && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

COPY --from=builder /app/ratchet .
ENTRYPOINT ["./ratchet"]
