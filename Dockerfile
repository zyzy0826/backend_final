FROM golang:1.25-alpine AS builder

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o server ./cmd/server

# ---

# 3.22 ships docker-cli 28.x. The version matters: the judge mounts a single
# directory out of the shared storage volume with `--mount ...,volume-subpath=`,
# which the CLI has only been able to parse since 25.0 (Alpine 3.19 was on 24.x).
FROM alpine:3.22

# docker-cli: the judge spawns sibling containers through the mounted
#   /var/run/docker.sock (DooD), which requires the docker client binary.
# openssl:    the keygen step generates the JWT EC key pair inside this image,
#   so no OpenSSL is needed on the host.
RUN apk add --no-cache ca-certificates docker-cli openssl

WORKDIR /app

COPY --from=builder /build/server .
COPY scripts/ ./scripts/
RUN chmod +x ./scripts/*.sh

EXPOSE 8080

CMD ["./server"]
