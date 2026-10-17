# Тулчейн задан в go.mod (toolchain go1.27.2); при GOTOOLCHAIN=auto любой
# установленный go >= 1.21 скачает нужную версию сам.
GO ?= go
# Не LINT: у GNU make это встроенная переменная (= lint), и ?= её не
# перекрывает.
GOLANGCI_LINT ?= golangci-lint

# Настройки разработки из ../.env (если файл есть) — в окружение команд.
# Само приложение .env не читает: в контейнере переменные задаёт compose.
-include ../.env
export

.PHONY: test test-live lint build run-api run-worker migrate

test:
	$(GO) test ./...

test-live:
	LINGUASTACK_LIVE_OLLAMA=1 $(GO) test -run Live ./...

lint:
	$(GOLANGCI_LINT) run ./...

build:
	$(GO) build -o bin/ ./cmd/...

run-api:
	$(GO) run ./cmd/api

run-worker:
	$(GO) run ./cmd/worker

migrate:
	$(GO) run ./cmd/api -migrate
