.PHONY: run build setup gen-keys docker-up docker-down migrate tidy

run:
	go run ./cmd/server

build:
	go build -o bin/server ./cmd/server

tidy:
	go mod tidy

setup: tidy gen-keys
	mkdir -p storage/uploads storage/workspace

gen-keys:
	mkdir -p keys
	openssl genpkey -algorithm EC -pkeyopt ec_paramgen_curve:P-256 -out keys/private.pem
	openssl pkey -in keys/private.pem -pubout -out keys/public.pem

docker-up:
	docker compose up -d --build

docker-down:
	docker compose down -v

migrate:
	psql $(DATABASE_URL) -f internal/db/schema.sql
