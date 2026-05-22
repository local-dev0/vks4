# vks4 — корневой Makefile
SHELL := /usr/bin/env bash
.DEFAULT_GOAL := help

GO_SERVICES := control-plane signaling media-router media-worker sip-gateway
WEB_DIR     := web/admin

.PHONY: help
help: ## список целей
	@awk 'BEGIN{FS=":.*## "} /^[a-zA-Z_-]+:.*## /{printf "  \033[36m%-18s\033[0m %s\n",$$1,$$2}' $(MAKEFILE_LIST)

.PHONY: gen
gen: gen-openapi gen-proto gen-sqlc ## кодогенерация

gen-openapi: ## openapi → ts типы для admin
	cd $(WEB_DIR) && npm run gen:openapi

gen-proto: ## protoc → go из api/proto
	@command -v buf >/dev/null || (echo "buf not found, see https://buf.build" && exit 1)
	buf generate

gen-sqlc: ## sqlc → go из services/*/sql
	@command -v sqlc >/dev/null || (echo "sqlc not found" && exit 1)
	cd services/control-plane && sqlc generate

.PHONY: lint
lint: lint-go lint-web ## линт всех

lint-go:
	@for s in $(GO_SERVICES); do echo "→ lint $$s"; (cd services/$$s && golangci-lint run ./...) || exit 1; done

lint-web:
	cd $(WEB_DIR) && npm run lint

.PHONY: test
test: test-go test-web ## тесты

test-go:
	@for s in $(GO_SERVICES); do echo "→ test $$s"; (cd services/$$s && go test ./...) || exit 1; done

test-web:
	cd $(WEB_DIR) && npm test --silent

.PHONY: docker
docker: ## сборка всех образов
	docker compose build

.PHONY: up
up: ## docker compose up -d
	docker compose up -d

.PHONY: down
down: ## docker compose down
	docker compose down

.PHONY: logs
logs:
	docker compose logs -f --tail=200

.PHONY: ps
ps:
	docker compose ps

.PHONY: migrate-up
migrate-up: ## миграции БД
	docker compose run --rm control-plane migrate up

.PHONY: migrate-down
migrate-down:
	docker compose run --rm control-plane migrate down

.PHONY: e2e
e2e: ## smoke-тест: поднять стек и прогнать load-tester
	docker compose up -d
	docker compose run --rm load-tester
	docker compose down

.PHONY: keys
keys: ## сгенерировать JWT RS256 ключи (dev)
	mkdir -p secrets
	openssl genpkey -algorithm RSA -out secrets/jwt_private.pem -pkeyopt rsa_keygen_bits:2048
	openssl rsa -in secrets/jwt_private.pem -pubout -out secrets/jwt_public.pem
	@echo "Keys written to secrets/ (gitignored)."

.PHONY: clean
clean:
	rm -rf dist bin
	@for s in $(GO_SERVICES); do (cd services/$$s && go clean ./... 2>/dev/null || true); done
	cd $(WEB_DIR) && rm -rf dist .vite node_modules/.cache
