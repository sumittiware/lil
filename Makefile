.PHONY: build run

BIN := bin/lil.bin

LAST_COMMIT := $(shell git rev-parse --short HEAD)
LAST_COMMIT_DATE := $(shell git show -s --format=%ci ${LAST_COMMIT})
VERSION := $(shell git describe --tags)
BUILDSTR := ${VERSION} (Commit: ${LAST_COMMIT_DATE} (${LAST_COMMIT}), Build: $(shell date +"%Y-%m-%d% %H:%M:%S %z"))

.PHONY: build-ui
build-ui:
	cd ui && pnpm install && pnpm build

.PHONY: build
build: build-ui
	CGO_ENABLED=0 go build -o ${BIN} -ldflags="-X 'main.buildString=${BUILDSTR}'" .

.PHONY: run
run: build ## Run binary.
	./${BIN}

.PHONY: clean
clean: ## Remove temporary files and the `bin` folder.
	rm -rf bin

.PHONY: dev-up
dev-up: ## Start development environment
	docker network create lil-dev-network || true
	docker compose -f dev/docker-compose.yml -f dev/compose-plausible.yml up --build -d

.PHONY: dev-down
dev-down: ## Stop development environment
	docker compose -f dev/docker-compose.yml -f dev/compose-plausible.yml down

.PHONY: dev-logs
dev-logs: ## View development logs
	docker compose -f dev/docker-compose.yml -f dev/compose-plausible.yml logs -f

.PHONY: hosts-entry
hosts-entry: ## Add required entries to /etc/hosts
	@echo "Adding entries to /etc/hosts..."
	@sudo sh -c 'echo "127.0.0.1 lil.internal plausible.internal" >> /etc/hosts'
