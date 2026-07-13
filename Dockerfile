FROM golang:1.25-alpine AS builder

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o server ./cmd/server

# ---

FROM alpine:3.19

# docker-cli: the judge spawns sibling containers through the mounted
# /var/run/docker.sock (DooD), which requires the docker client binary.
RUN apk add --no-cache ca-certificates docker-cli

WORKDIR /app

COPY --from=builder /build/server .

EXPOSE 8080

CMD ["./server"]
