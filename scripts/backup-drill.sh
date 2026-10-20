#!/usr/bin/env bash
# Учебная тревога (ТЗ §13, §14; дополнение §18): поднять отдельный стенд,
# наполнить его через API, сделать бэкап, стереть тома, восстановить и
# проверить, что вход, книга с картинкой, позиция чтения, перевод и экспорт
# на месте. Чужие контейнеры и рабочую базу не трогает: свой проект compose
# (lsdrill) и свои порты. Стенд убирается за собой.
#
#   scripts/backup-drill.sh
#   DRILL_DB_PORT=55434 DRILL_API_PORT=58000 scripts/backup-drill.sh
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT"
export COMPOSE_PROJECT_NAME=lsdrill
export COMPOSE_FILE="$ROOT/docker-compose.yml:$ROOT/scripts/drill.compose.yml"
export LINGUASTACK_DB_PORT=${DRILL_DB_PORT:-55434}
export LINGUASTACK_PUBLISH_PORT=${DRILL_API_PORT:-58000}
export LINGUASTACK_IMAGE=${LINGUASTACK_IMAGE:-linguastack:drill}
API=http://127.0.0.1:$LINGUASTACK_PUBLISH_PORT
WORK=$(mktemp -d)
JAR=$WORK/cookies
EMAIL=drill@example.com
PASSWORD=drill-password-for-restore

cleanup() {
  docker compose down -v --remove-orphans >/dev/null 2>&1 || true
  rm -rf "$WORK"
}
trap cleanup EXIT

step() { printf '\n== %s\n' "$*"; }
fail() { echo "ПРОВАЛ: $*" >&2; exit 1; }

# api МЕТОД ПУТЬ [аргументы curl] — запрос с cookie стенда, тело ответа в stdout.
api() {
  local method=$1 path=$2
  shift 2
  curl -sS --fail-with-body -b "$JAR" -c "$JAR" -X "$method" "$API$path" "$@"
}
json() { api "$1" "$2" -H 'Content-Type: application/json' -d "$3"; }
field() { python3 -c 'import json,sys; v=json.load(sys.stdin); [v:=v[int(k) if k.isdigit() else k] for k in sys.argv[1].split(".")]; print(v)' "$1"; }

wait_status() { # путь поле ожидаемое
  for _ in $(seq 120); do
    status=$(api GET "$1" | field "$2")
    [[ $status == "$3" ]] && return 0
    [[ $status == failed ]] && fail "$1: статус failed"
    sleep 1
  done
  fail "$1: статус $status, ждали $3"
}

step "стенд $COMPOSE_PROJECT_NAME: сборка и запуск"
# Сначала сборка: иначе compose пытается скачать образ worker-а и bot-а из
# реестра раньше, чем его соберёт api.
docker compose build
docker compose up -d --wait

step "наполнение через API"
json POST /api/auth/register "{\"email\":\"$EMAIL\",\"password\":\"$PASSWORD\"}" >/dev/null
json POST /api/auth/login "{\"email\":\"$EMAIL\",\"password\":\"$PASSWORD\"}" >/dev/null
# Книга HTML с картинкой в data: — картинка становится ресурсом книги.
PNG=iVBORw0KGgoAAAANSUhEUgAAAAIAAAACCAIAAAD91JpzAAAAEElEQVR4nGP4zwAE/xkgFAAb8gP91pbyKwAAAABJRU5ErkJggg==
cat >"$WORK/book.html" <<HTML
<!doctype html><html lang="en"><head><title>Drill Book</title></head><body>
<h1>Chapter One</h1><p>The first paragraph.</p><p><img src="data:image/png;base64,$PNG" alt="dot"></p>
<p>The second paragraph.</p></body></html>
HTML
BOOK=$(api POST /api/books -F "file=@$WORK/book.html;type=text/html" | field id)
api GET "/api/books/$BOOK/assets/embedded/img-0001.png" -o "$WORK/img-before.png"
NODE=$(api GET "/api/books/$BOOK/chapters/0" | field blocks.1.node_id)
CONFIG=$(json POST /api/provider-configs '{"provider_id":"fake","name":"Учебная тревога"}' | field id)
PROJECT=$(json POST "/api/books/$BOOK/translations" \
  "{\"source_lang\":\"en\",\"target_lang\":\"ru\",\"provider_config_id\":\"$CONFIG\",\"model_id\":\"fake-1\"}" | field id)
wait_status "/api/translations/$PROJECT" status completed
json PUT "/api/books/$BOOK/position" \
  "{\"chapter_index\":0,\"node_id\":\"$NODE\",\"translation_id\":\"$PROJECT\",\"mode\":\"parallel\"}" >/dev/null
EXPORT=$(json POST "/api/books/$BOOK/exports" "{\"translation_id\":\"$PROJECT\",\"format\":\"epub\",\"mode\":\"bilingual\"}" | field id)
wait_status "/api/exports/$EXPORT" status completed
api GET "/api/exports/$EXPORT/file" -o "$WORK/export-before.epub"

step "бэкап"
"$ROOT/scripts/backup.sh" "$WORK/backups"
BACKUP=$(find "$WORK/backups" -maxdepth 1 -type d -name 'linguastack-*' | head -1)

step "авария: тома стёрты"
docker compose down -v
docker compose up -d --wait
rm -f "$JAR"
code=$(curl -sS -o /dev/null -w '%{http_code}' -H 'Content-Type: application/json' \
  -d "{\"email\":\"$EMAIL\",\"password\":\"$PASSWORD\"}" "$API/api/auth/login")
[[ $code == 401 ]] || fail "после стирания вход должен не работать, получено $code"

step "восстановление"
"$ROOT/scripts/restore.sh" --yes "$BACKUP"

step "проверка"
json POST /api/auth/login "{\"email\":\"$EMAIL\",\"password\":\"$PASSWORD\"}" >/dev/null || fail "вход тем же паролем"
[[ $(api GET /api/books | field 0.id) == "$BOOK" ]] || fail "книги нет в списке"
api GET "/api/books/$BOOK/assets/embedded/img-0001.png" -o "$WORK/img-after.png"
cmp -s "$WORK/img-before.png" "$WORK/img-after.png" || fail "картинка книги отличается"
[[ $(api GET "/api/books/$BOOK/position" | field node_id) == "$NODE" ]] || fail "позиция чтения потеряна"
[[ $(api GET "/api/translations/$PROJECT" | field status) == completed ]] || fail "перевод потерян"
api GET "/api/translations/$PROJECT/segments" | grep -q 'RU:' || fail "сегменты перевода потеряны"
api GET "/api/exports/$EXPORT/file" -o "$WORK/export-after.epub"
cmp -s "$WORK/export-before.epub" "$WORK/export-after.epub" || fail "файл экспорта отличается"
# Восстановленное хранилище доступно на запись: новый экспорт собирается.
AGAIN=$(json POST "/api/books/$BOOK/exports" "{\"translation_id\":\"$PROJECT\",\"format\":\"markdown\",\"mode\":\"translation\"}" | field id)
wait_status "/api/exports/$AGAIN" status completed

echo
echo "учебная тревога пройдена: копия восстанавливается полностью"
