#!/usr/bin/env bash
# Резервная копия LinguaStack (дополнение §18): база — pg_dump -Fc, файлы —
# архив тома storage. Запуск на сервере из каталога с docker-compose.yml:
#
#   scripts/backup.sh [каталог]        # по умолчанию ./backups
#   KEEP=30 scripts/backup.sh /srv/backups
#
# Копия — каталог linguastack-<время UTC> с db.dump и storage.tar.gz;
# хранятся последние KEEP (14). Дамп проверяется: pg_restore --list должен
# его прочитать. Сервис не останавливается: книга, загруженная в минуту
# бэкапа, может оказаться в архиве без строки в базе — это безвредно.
# Проект и файлы compose — как у docker compose: COMPOSE_PROJECT_NAME,
# COMPOSE_FILE.
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT"
DEST=${1:-./backups}
KEEP=${KEEP:-14}
[[ $KEEP =~ ^[1-9][0-9]*$ ]] || { echo "KEEP — целое число больше 0" >&2; exit 2; }

container() {
  local id
  id=$(docker compose ps -a -q "$1")
  [[ -n $id ]] || { echo "контейнер $1 не найден: стек не поднят (docker compose up -d)" >&2; exit 1; }
  echo "$id"
}

# Том storage — тот, что смонтирован в api; образ api без оболочки (distroless),
# поэтому архив делает одноразовый контейнер из образа БД (в нём есть tar).
API=$(container api)
DB=$(container db)
VOLUME=$(docker inspect -f '{{range .Mounts}}{{if eq .Destination "/data/storage"}}{{.Name}}{{end}}{{end}}' "$API")
HELPER=$(docker inspect -f '{{.Config.Image}}' "$DB")
[[ -n $VOLUME ]] || { echo "у api нет тома /data/storage" >&2; exit 1; }

stamp=$(date -u +%Y%m%dT%H%M%SZ)
mkdir -p "$DEST"
out="$DEST/linguastack-$stamp"
tmp="$out.partial"
rm -rf "$tmp"
mkdir "$tmp"
trap 'rm -rf "$tmp"' EXIT

echo "бэкап базы…"
docker compose exec -T db pg_dump -U linguastack -d linguastack -Fc >"$tmp/db.dump"
docker compose exec -T db pg_restore --list >/dev/null <"$tmp/db.dump"

echo "бэкап файлов (том $VOLUME)…"
docker run --rm -v "$VOLUME":/data/storage:ro "$HELPER" tar czf - -C /data storage >"$tmp/storage.tar.gz"
gzip -t "$tmp/storage.tar.gz"

mv "$tmp" "$out"
trap - EXIT
echo "готово: $out ($(du -sh "$out" | cut -f1))"

# Ротация: старые копии сверх KEEP удаляются, начиная с самых ранних.
mapfile -t all < <(find "$DEST" -maxdepth 1 -type d -name 'linguastack-*' ! -name '*.partial' | sort)
excess=$((${#all[@]} - KEEP))
for ((i = 0; i < excess; i++)); do
  echo "удаляю старую копию ${all[i]}"
  rm -rf "${all[i]}"
done
