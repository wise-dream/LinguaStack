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

* Python 3.10 (целевая версия — см. дополнение §0.1)
* Docker и Docker Compose
* Ollama — для локальных моделей

## Запуск для разработки

```bash
# 1. База данных
docker compose up -d db

# Дождаться готовности именно целевой БД, а не только сервера:
# pg_isready может ответить «готов» ещё во время initdb.
until docker compose exec -T db psql -U linguastack -d linguastack -c 'select 1' >/dev/null 2>&1; do sleep 1; done

# 2. Зависимости
cd backend
python3 -m venv .venv
.venv/bin/pip install -e ".[dev]"

# 3. Настройки
cp ../.env.example ../.env
# Сгенерировать ключ шифрования пользовательских API-ключей:
.venv/bin/python -c "from cryptography.fernet import Fernet; print(Fernet.generate_key().decode())"
# и вписать его в LINGUASTACK_SECRET_KEY

# 4. Приложение
.venv/bin/uvicorn linguastack.main:app --reload
```

Postgres публикуется на **127.0.0.1:5434** — только на localhost и не на 5432/5433,
которые на машине разработки заняты другими проектами.

## Тесты

```bash
cd backend
.venv/bin/python -m pytest              # без обращений к сети
.venv/bin/ruff check src tests

# Тесты против живой модели (требуют запущенного Ollama):
LINGUASTACK_LIVE_OLLAMA=1 .venv/bin/python -m pytest -m live
```

## Статус

В работе: этапы 1–3 ТЗ (фундамент, импорт, перевод). Веб-интерфейс, PDF/DOCX,
экспорт и Telegram-бот — последующие этапы.
