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

RUN apt update && apt install -y ca-certificates && update-ca-certificates

COPY --from=builder /app/ratchet .

ENTRYPOINT ["./ratchet"]
