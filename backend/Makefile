# Тулчейн задан в go.mod (toolchain go1.27.2); при GOTOOLCHAIN=auto любой
# установленный go >= 1.21 скачает нужную версию сам.
GO ?= go
LINT ?= golangci-lint

.PHONY: test test-live lint build run-api migrate

test:
	$(GO) test ./...

test-live:
	LINGUASTACK_LIVE_OLLAMA=1 $(GO) test -run Live ./...

lint:
	$(LINT) run ./...

build:
	$(GO) build -o bin/ ./cmd/...

run-api:
	$(GO) run ./cmd/api

migrate:
	$(GO) run ./cmd/api -migrate
