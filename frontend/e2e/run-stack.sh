#!/usr/bin/env bash
# Стенд сквозных тестов: собранное веб-приложение, настоящие api и worker
# с тестовым провайдером fake на чистой базе linguastack_e2e.
# Запускает Playwright (webServer в playwright.config.ts); останавливается
# вместе с ним.
set -euo pipefail

HERE=$(cd "$(dirname "$0")" && pwd)
FRONTEND=$(dirname "$HERE")
BACKEND=$(dirname "$FRONTEND")/backend
WORK=$(mktemp -d)
ADMIN_URL=${LINGUASTACK_E2E_ADMIN_URL:-postgres://linguastack:linguastack@127.0.0.1:5434/linguastack?sslmode=disable}
DB_URL=${ADMIN_URL/\/linguastack\?/\/linguastack_e2e?}

sql() {
  if command -v psql >/dev/null; then
    psql "$ADMIN_URL" -v ON_ERROR_STOP=1 -qc "$1"
  else
    # Разработка: psql есть в контейнере БД из docker-compose.yml.
    docker compose -f "$(dirname "$BACKEND")/docker-compose.yml" exec -T db psql -U linguastack -d linguastack -v ON_ERROR_STOP=1 -qc "$1"
  fi
}

sql 'DROP DATABASE IF EXISTS linguastack_e2e'
sql 'CREATE DATABASE linguastack_e2e'

(cd "$FRONTEND" && npm run -s build)
(cd "$BACKEND" && go build -o "$WORK/" ./cmd/api ./cmd/worker)

export LINGUASTACK_DATABASE_URL=$DB_URL
export LINGUASTACK_STORAGE_ROOT=$WORK/storage
export LINGUASTACK_HTTP_ADDR=127.0.0.1:8090
export LINGUASTACK_WEB_ROOT=$FRONTEND/dist
export LINGUASTACK_SESSION_COOKIE_SECURE=false
export LINGUASTACK_E2E_FAKE_PROVIDER=true
export LINGUASTACK_E2E_FAKE_DELAY=${LINGUASTACK_E2E_FAKE_DELAY:-400ms}
export LINGUASTACK_MAX_BATCH_SEGMENTS=2
# Каждый тест регистрирует своих пользователей с 127.0.0.1 — лимит
# регистраций с одного адреса стенду не нужен.
export LINGUASTACK_REGISTER_PER_HOUR=0
# Telegram «включён» только для api: страница настроек выдаёт ссылку
# привязки. Процесс bot на стенде не запускается — в Telegram ничего не уходит.
export LINGUASTACK_TELEGRAM_BOT_TOKEN=000000:e2e-not-a-real-token-for-tests-only
export LINGUASTACK_TELEGRAM_BOT_USERNAME=linguastack_e2e_bot

"$WORK/api" -migrate
"$WORK/api" &
API=$!
"$WORK/worker" &
WORKER=$!
trap 'kill $API $WORKER 2>/dev/null; wait; rm -rf "$WORK"' EXIT INT TERM
wait -n
