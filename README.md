# LinguaStack

Многопользовательская платформа для перевода и чтения книг с использованием
локальных (Ollama) и облачных языковых моделей.

Цикл работы: **импорт → структурирование → перевод → проверка → чтение → экспорт.**

## Документы

| Документ | Назначение |
|---|---|
| [`PROJECT`](PROJECT) | Техническое задание (v1.1) |
| [`docs/spec/SPEC-ADDENDUM.md`](docs/spec/SPEC-ADDENDUM.md) | Дополнение: решения, которые ТЗ оставил открытыми |
| [`docs/superpowers/plans/`](docs/superpowers/plans/) | План реализации по задачам |

## Требования

* Go — язык `1.26`, тулчейн `go1.27.2` (задан в `backend/go.mod`; при
  `GOTOOLCHAIN=auto` установленный go ≥ 1.21 скачает его сам)
* Docker и Docker Compose
* Ollama — для локальных моделей
* golangci-lint v2, собранный тем же тулчейном:
  `GOTOOLCHAIN=go1.27.2 go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0`

> На машине разработки `/usr/bin/go` — это gccgo 1.16, он не понимает
> современный `go.mod`. Нужный `go` лежит в `/usr/local/go/bin`, а линтер
> v2 — в `/root/go/bin`: `export PATH=/root/go/bin:/usr/local/go/bin:$PATH`.

## Запуск для разработки

```bash
# 1. База данных
docker compose up -d db

# Дождаться готовности именно целевой БД, а не только сервера:
# pg_isready может ответить «готов» ещё во время initdb.
until docker compose exec -T db psql -U linguastack -d linguastack -c 'select 1' >/dev/null 2>&1; do sleep 1; done

# 2. Настройки
cp .env.example .env
openssl rand -base64 32   # вписать в LINGUASTACK_SECRET_KEY

# 3. Сервер
cd backend
make run-api
```

Postgres публикуется на **127.0.0.1:5434** — только на localhost и не на 5432/5433,
которые на машине разработки заняты другими проектами.

## Тесты

```bash
cd backend
make test        # без обращений к сети
make lint

# Тесты против живой модели (требуют запущенного Ollama):
make test-live
```

## Статус

В работе: этапы 1–3 ТЗ (фундамент, импорт, перевод). Веб-интерфейс, PDF/DOCX,
экспорт и Telegram-бот — последующие этапы.
