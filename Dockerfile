# Stage 1: Build the Go application
FROM golang:1.24 AS builder

ENV GOTOOLCHAIN=auto
ENV CGO_ENABLED=0

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build ./cmd/ratchet
RUN ./ratchet --version

# Stage 2: Create the final image
FROM public.ecr.aws/lts/ubuntu:edge AS runner

RUN apt-get update && \
    if dpkg --compare-versions "$(dpkg-query -f '${Version}' -W libc-bin)" lt "2.39-0ubuntu8.4"; then \
        apt-get install -y --no-install-recommends libc-bin=2.39-0ubuntu8.4; \
    fi && \
    if dpkg --compare-versions "$(dpkg-query -f '${Version}' -W libtasn1-6)" lt "4.19.0-3ubuntu0.24.04.1"; then \
        apt-get install -y --no-install-recommends libtasn1-6=4.19.0-3ubuntu0.24.04.1; \
    fi && \
    if dpkg --compare-versions "$(dpkg-query -f '${Version}' -W libgnutls30)" lt "3.8.3-1.1ubuntu3.3"; then \
        apt-get install -y --no-install-recommends libgnutls30=3.8.3-1.1ubuntu3.3; \
    fi && \
    if dpkg --compare-versions "$(dpkg-query -f '${Version}' -W libcap2)" lt "1:2.66-5ubuntu2.2"; then \
        apt-get install -y --no-install-recommends libcap2=1:2.66-5ubuntu2.2; \
    fi && \
    apt-get install -y --no-install-recommends ca-certificates && \
    update-ca-certificates && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

COPY --from=builder /app/ratchet .
ENTRYPOINT ["./ratchet"]
