# Every target runs inside Docker — no Go toolchain, OpenSSL or psql is needed
# on the host. `make up` is all it takes to get a working stack.
.PHONY: up down clean restart rebuild logs ps shell psql gen-keys \
        test vet tidy build export-storage

## --- Stack lifecycle ---

up:            ## Build and start the whole stack (keys and schema handled automatically)
	docker compose up -d --build

down:          ## Stop the stack, keep the database, keys and storage
	docker compose down

clean:         ## Stop the stack and delete ALL volumes (database, keys, storage)
	docker compose down -v

restart:       ## Restart just the app container
	docker compose restart app

rebuild:       ## Rebuild the app image and recreate the container
	docker compose up -d --build app

## --- Inspection ---

logs:          ## Follow the app logs
	docker compose logs -f app

ps:            ## Show container status
	docker compose ps

shell:         ## Shell inside the app container (storage lives at /app/storage)
	docker compose exec app sh

psql:          ## psql session against the database
	docker compose exec db psql -U regs -d regs

export-storage: ## Copy the storage volume out to ./storage-export for inspection
	docker compose cp app:/app/storage ./storage-export

## --- Development (Go runs in a throwaway container) ---

build:         ## Build the app image
	docker compose build

test:
	docker compose run --rm tools test ./...

vet:
	docker compose run --rm tools vet ./...

tidy:
	docker compose run --rm tools mod tidy

gen-keys:      ## Generate the JWT key pair if the keys volume is empty
	docker compose run --rm keygen
