#!/usr/bin/env bash
# Восстановление LinguaStack из копии scripts/backup.sh (дополнение §18):
#
#   scripts/restore.sh backups/linguastack-20261020T030000Z
#   scripts/restore.sh --yes <копия>     # без вопроса (для автоматизации)
#
# ТЕКУЩИЕ ДАННЫЕ СТИРАЮТСЯ: база пересоздаётся из дампа, том storage —
# из архива. На время восстановления api, worker и bot останавливаются,
# затем стек запускается снова. Ключ шифрования (LINGUASTACK_SECRET_KEY)
# в копию не входит — нужен тот же, что был: иначе сохранённые ключи
# провайдеров не расшифровать.
set -euo pipefail

yes=false
if [[ ${1:-} == --yes || ${1:-} == -y ]]; then
  yes=true
  shift
fi
[[ $# -eq 1 ]] || { echo "использование: $0 [--yes] <каталог копии>" >&2; exit 2; }
SRC=$(cd "$1" && pwd)
[[ -s $SRC/db.dump && -s $SRC/storage.tar.gz ]] || { echo "$SRC: нет db.dump или storage.tar.gz" >&2; exit 1; }

ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT"

if ! $yes; then
  read -r -p "Восстановление сотрёт текущие базу и файлы и заменит их копией $SRC. Продолжить? [y/N] " answer
  [[ $answer == y || $answer == Y ]] || { echo "отменено"; exit 1; }
fi

container() {
  local id
  id=$(docker compose ps -a -q "$1")
  [[ -n $id ]] || { echo "контейнер $1 не найден: сначала docker compose up -d" >&2; exit 1; }
  echo "$id"
}
API=$(container api)
VOLUME=$(docker inspect -f '{{range .Mounts}}{{if eq .Destination "/data/storage"}}{{.Name}}{{end}}{{end}}' "$API")
[[ -n $VOLUME ]] || { echo "у api нет тома /data/storage" >&2; exit 1; }

echo "останавливаю api, worker, bot…"
docker compose stop api worker bot
docker compose up -d --wait db
HELPER=$(docker inspect -f '{{.Config.Image}}' "$(container db)")

echo "восстанавливаю базу…"
psql() { docker compose exec -T db psql -U linguastack -v ON_ERROR_STOP=1 -q "$@"; }
psql -d postgres -c 'DROP DATABASE IF EXISTS linguastack WITH (FORCE)'
psql -d postgres -c 'CREATE DATABASE linguastack OWNER linguastack'
docker compose exec -T db pg_restore -U linguastack -d linguastack --exit-on-error --no-owner <"$SRC/db.dump"

echo "восстанавливаю файлы (том $VOLUME)…"
# Владелец файлов — числовой uid процесса api (nonroot), tar от root его
# сохраняет.
docker run --rm -i -v "$VOLUME":/data/storage "$HELPER" \
  sh -c 'find /data/storage -mindepth 1 -delete && tar xzf - -C /data' <"$SRC/storage.tar.gz"

echo "запускаю стек…"
docker compose up -d --wait
echo "восстановлено из $SRC"
