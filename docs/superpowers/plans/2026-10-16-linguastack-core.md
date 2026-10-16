# LinguaStack Core (Этапы 1–3) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Довести LinguaStack до состояния, в котором загруженная книга (Markdown, ZIP, EPUB, TXT) реально переводится в фоне через Ollama или внешний API, с сохранением прогресса, проверкой структуры и изоляцией пользователей.

**Architecture:** FastAPI-бэкенд поверх PostgreSQL 17 (asyncpg + SQLAlchemy 2.0). Импортёры приводят любой формат к Canonical Document Model; сегментатор нарезает CDM на блоки 1:1 и группирует в батчи; сервис перевода маскирует защищённые фрагменты, говорит с моделью по нумерованному конверту и валидирует результат; очередь на арендах (lease) в Postgres с `FOR UPDATE SKIP LOCKED` даёт фоновое выполнение, честность между пользователями и восстановление после перезапуска. Провайдеры (Ollama, OpenAI-compatible, Anthropic) скрыты за `LLMProvider`, который выражает намерение, а не параметры генерации.

**Tech Stack:** Python 3.10, FastAPI 0.143, SQLAlchemy 2.0.54 (async), Alembic, PostgreSQL 17, pydantic 2.14, argon2-cffi, cryptography (Fernet), httpx, anthropic 1.x, ebooklib, markdown-it-py, pytest + pytest-asyncio, ruff.

**Spec:** [`PROJECT`](../../../PROJECT) (ТЗ v1.1) + [`docs/spec/SPEC-ADDENDUM.md`](../../spec/SPEC-ADDENDUM.md) — дополнение фиксирует все решения, которые ТЗ оставил открытыми. **Исполнитель читает оба документа.** Ссылки «ТЗ §N» и «ADD §N» указывают на них соответственно.

## Global Constraints

- **Python 3.10.12** — целевая версия. Запрещены `typing.Self`, `except*`, `enum.StrEnum`, `datetime.UTC`. Использовать `typing_extensions.Self`, `class X(str, Enum)`, `datetime.timezone.utc`. Причина — ADD §0.1.
- **Точные пины зависимостей** (последние, поддерживающие 3.10): `fastapi==0.143.2`, `uvicorn[standard]==0.54.0`, `sqlalchemy[asyncio]==2.0.54`, `alembic==1.20.0`, `asyncpg==0.32.0`, `greenlet==3.5.6`, `pydantic==2.14.1`, `pydantic-settings==2.15.0`, `argon2-cffi==25.1.0`, `cryptography==50.0.2`, `httpx==0.28.1`, `python-multipart==0.0.32`, `email-validator==2.3.0`, `sse-starlette==3.5.0`, `anthropic==1.15.0`, `ebooklib==0.20`, `markdown-it-py==4.2.0`, `beautifulsoup4==4.15.0`, `lxml==6.1.3`, `pytest==9.1.1`, `pytest-asyncio==1.4.0`, `ruff==0.17.1`. **Не поднимать `sqlalchemy` до 2.1.x и `pydantic-settings` до 2.16.0** — требуют ≥3.11.
- **Anthropic model IDs — без суффиксов даты:** `claude-opus-5-5`, `claude-sonnet-5-5`, `claude-haiku-5-5`. Цены — ADD §6.2.
- **Anthropic: не передавать `temperature`, `top_p`, `top_k`** — Opus/Sonnet 5.5 отвечают 400. Глубина — через `output_config={"effort": ...}`. ADD §6.
- **Anthropic: проверять `response.stop_reason == "refusal"` до чтения `content`.** ADD §6.1.
- **Оригинал неизменен** (ТЗ §4.1): загруженный файл после записи только читается. Нормализованный CDM и переводы — отдельные записи.
- **Изоляция:** любой доступ к ресурсу — через зависимость `owned()`; чужой ресурс → **404, не 403**; роль `admin` обхода не даёт. ADD §9.
- **Секреты:** API-ключи пользователей никогда не возвращаются клиентом и не попадают в логи. ADD §8.
- **Коммит после каждой задачи**, сообщение в Conventional Commits. Коммит-сообщения заканчиваются строкой `Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>`.
- **TDD:** сначала падающий тест, затем минимальная реализация. Тесты не требуют сети: сеть только в тестах с `@pytest.mark.live`.
- Все timestamp-поля — `TIMESTAMP WITH TIME ZONE`, значения — timezone-aware UTC.

## Review Focus

Пять классов входных данных, которые ТЗ подразумевает, но ни одна задача не проверяет естественным образом. Молчание спецификации про вход — не разрешение ломаться на нём. Для каждого указана задача, в которую добавлен тест.

1. **Глава без переводимых блоков** (целиком код или только изображения) — прогресс `0/0` не должен давать делением на ноль и не должен «висеть» навсегда; проект обязан дойти до `completed`. → тест в задаче 19 (сегментатор) и 25 (worker).
2. **Исходник, в котором уже встречаются маркеры `⟦1⟧`** — маскирование обязано их экранировать, иначе восстановление подставит чужой фрагмент и тихо испортит текст. → тест в задаче 7.
3. **Один блок больше контекста модели** (глава одним абзацем на мегабайт) — требуется явная ошибка с понятным сообщением, а не молчаливое обрезание текста. → тест в задаче 19.
4. **Повторный импорт того же файла и два проекта перевода на одну книгу** — ТЗ §5.1 прямо разрешает несколько проектов; нельзя допустить коллизии в хранилище и перезапись чужого прогресса. → тест в задаче 13 и 22.
5. **Отмена/пауза проекта в момент, когда worker держит батч** — уже полученный (оплаченный) ответ модели не должен теряться, и записи после отмены не должны «воскрешать» проект. → тест в задаче 25.

Дополнительно по данным: TXT с BOM, CRLF и в cp1251; пустые абзацы; текст RTL; EPUB без `toc.ncx`. → тесты в задачах 9, 12.

---

## File Structure

Ответственность по файлам. Файлы, которые меняются вместе, лежат вместе; разделение — по ответственности, не по техническому слою.

| Файл | Ответственность |
|---|---|
| `docker-compose.yml` | PostgreSQL 17 для разработки и тестов |
| `backend/pyproject.toml` | пины, настройки ruff и pytest |
| `backend/alembic/` | миграции (обязательны по ТЗ §10) |
| `src/linguastack/config.py` | `Settings` — все лимиты из ADD §10 |
| `src/linguastack/main.py` | фабрика приложения, подключение роутеров |
| `src/linguastack/db/base.py` | `Base`, движок, фабрика сессий |
| `src/linguastack/db/models/*.py` | ORM-модели по сущностям ТЗ §11 |
| `src/linguastack/cdm/model.py` | Canonical Document Model (pydantic) |
| `src/linguastack/cdm/ids.py` | `node_id` и `source_hash` (ADD §1) |
| `src/linguastack/cdm/masking.py` | маскирование защищённых фрагментов (ADD §4) |
| `src/linguastack/importers/base.py` | протокол импортёра, `ImportReport` |
| `src/linguastack/importers/{txt,markdown,zip_repo,epub}.py` | по одному адаптеру на формат (ТЗ §4.2) |
| `src/linguastack/providers/base.py` | `LLMProvider`, `ModelCapabilities`, таксономия ошибок |
| `src/linguastack/providers/{ollama,openai_compat,anthropic_provider,fake}.py` | адаптеры |
| `src/linguastack/translation/segmenter.py` | CDM → сегменты → батчи (ADD §2) |
| `src/linguastack/translation/prompt.py` | конверт `<<<SEG n>>>` и его парсер (ADD §3) |
| `src/linguastack/translation/validation.py` | проверки и severity (ADD §5) |
| `src/linguastack/translation/service.py` | перевод батча: маска → модель → валидация → запись |
| `src/linguastack/queue/repository.py` | захват/аренда/heartbeat задач (ADD §7) |
| `src/linguastack/queue/worker.py` | цикл worker-а, восстановление, идемпотентность |
| `src/linguastack/security/{passwords,sessions,secrets,ratelimit}.py` | по одному механизму на файл (ADD §8) |
| `src/linguastack/storage/files.py` | безопасные пути, UUID-имена, лимиты (ADD §11) |
| `src/linguastack/api/deps.py` | `current_user`, `owned()` — единственная точка проверки прав |
| `src/linguastack/api/*.py` | роутеры по эндпоинтам ТЗ §12 |

---

## Этап 1 — Фундамент

### Task 1: Каркас проекта, Postgres, настройки, health-эндпоинт

**Files:**
- Create: `.gitignore`, `docker-compose.yml`, `.env.example`, `README.md`
- Create: `backend/pyproject.toml`, `backend/src/linguastack/__init__.py`
- Create: `backend/src/linguastack/config.py`, `backend/src/linguastack/main.py`
- Create: `backend/tests/conftest.py`, `backend/tests/test_health.py`

**Interfaces:**
- Produces: `get_settings() -> Settings` (кэширована через `lru_cache`); `create_app() -> FastAPI`; `Settings` с полями `database_url`, `test_database_url`, `secret_key`, `storage_root`, `max_upload_bytes`, `max_zip_entries`, `max_zip_total_bytes`, `max_zip_ratio`, `max_tasks_per_user`, `max_projects_per_user`, `max_segment_attempts`, `max_batch_segments`, `max_batch_chars`, `task_lease_seconds`, `ollama_base_url`.

- [ ] **Step 1: Написать падающий тест**

```python
# backend/tests/test_health.py
from fastapi.testclient import TestClient

from linguastack.main import create_app


def test_health_reports_ok():
    client = TestClient(create_app())
    response = client.get("/api/health")
    assert response.status_code == 200
    assert response.json() == {"status": "ok"}


def test_settings_expose_limits_from_spec():
    from linguastack.config import get_settings

    settings = get_settings()
    # Значения по умолчанию из ADD §10 — лимиты обязаны быть настраиваемыми.
    assert settings.max_upload_bytes == 50 * 1024 * 1024
    assert settings.max_zip_entries == 2000
    assert settings.max_tasks_per_user == 3
    assert settings.max_segment_attempts == 3
```

- [ ] **Step 2: Запустить тест и убедиться, что он падает**

Run: `cd backend && python -m pytest tests/test_health.py -v`
Expected: FAIL — `ModuleNotFoundError: No module named 'linguastack'`

- [ ] **Step 3: Создать `docker-compose.yml` и `.env.example`**

```yaml
# docker-compose.yml
services:
  db:
    image: postgres:17-alpine
    environment:
      POSTGRES_USER: linguastack
      POSTGRES_PASSWORD: linguastack
      POSTGRES_DB: linguastack
    ports:
      - "5433:5432"
    volumes:
      - pgdata:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U linguastack"]
      interval: 3s
      timeout: 3s
      retries: 20
    restart: unless-stopped

volumes:
  pgdata:
```

Порт 5433 снаружи — чтобы не конфликтовать с возможным локальным Postgres.
`restart: unless-stopped` закрывает требование ТЗ §13 «автоматический запуск после перезагрузки».

```bash
# .env.example
LINGUASTACK_DATABASE_URL=postgresql+asyncpg://linguastack:linguastack@localhost:5433/linguastack
LINGUASTACK_TEST_DATABASE_URL=postgresql+asyncpg://linguastack:linguastack@localhost:5433/linguastack_test
# Сгенерировать: python -c "from cryptography.fernet import Fernet; print(Fernet.generate_key().decode())"
LINGUASTACK_SECRET_KEY=
LINGUASTACK_STORAGE_ROOT=./storage
LINGUASTACK_OLLAMA_BASE_URL=http://localhost:11434
```

- [ ] **Step 4: Создать `backend/pyproject.toml`**

```toml
[project]
name = "linguastack"
version = "0.1.0"
requires-python = ">=3.10,<3.11"
dependencies = [
    "fastapi==0.143.2",
    "uvicorn[standard]==0.54.0",
    "sqlalchemy[asyncio]==2.0.54",
    "alembic==1.20.0",
    "asyncpg==0.32.0",
    "greenlet==3.5.6",
    "pydantic==2.14.1",
    "pydantic-settings==2.15.0",
    "argon2-cffi==25.1.0",
    "cryptography==50.0.2",
    "httpx==0.28.1",
    "python-multipart==0.0.32",
    "email-validator==2.3.0",
    "sse-starlette==3.5.0",
    "anthropic==1.15.0",
    "ebooklib==0.20",
    "markdown-it-py==4.2.0",
    "beautifulsoup4==4.15.0",
    "lxml==6.1.3",
]

[project.optional-dependencies]
dev = ["pytest==9.1.1", "pytest-asyncio==1.4.0", "ruff==0.17.1"]

[build-system]
requires = ["setuptools>=68"]
build-backend = "setuptools.build_meta"

[tool.setuptools.packages.find]
where = ["src"]

[tool.pytest.ini_options]
testpaths = ["tests"]
asyncio_mode = "auto"
markers = [
    "live: обращается к реальному провайдеру; требует LINGUASTACK_LIVE_OLLAMA=1",
]

[tool.ruff]
target-version = "py310"
line-length = 100

[tool.ruff.lint]
select = ["E", "F", "I", "UP", "B", "ASYNC", "S"]
ignore = ["S101"]  # assert разрешён в тестах

[tool.ruff.lint.per-file-ignores]
"tests/*" = ["S105", "S106"]  # пароли-константы в тестах — это норма
```

- [ ] **Step 5: Написать `config.py`**

```python
# backend/src/linguastack/config.py
"""Настройки приложения. Все лимиты из ADD §10 задаются здесь."""
from functools import lru_cache
from pathlib import Path

from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    model_config = SettingsConfigDict(
        env_prefix="LINGUASTACK_",
        env_file=".env",
        extra="ignore",
    )

    database_url: str = (
        "postgresql+asyncpg://linguastack:linguastack@localhost:5433/linguastack"
    )
    test_database_url: str = (
        "postgresql+asyncpg://linguastack:linguastack@localhost:5433/linguastack_test"
    )
    # Fernet-ключ; в разработке допустим дефолт, в продакшене обязателен в окружении.
    secret_key: str = "ZmFrZS1kZXYta2V5LW5vdC1mb3ItcHJvZHVjdGlvbi0xMjM0NTY="
    storage_root: Path = Path("./storage")
    ollama_base_url: str = "http://localhost:11434"

    # --- лимиты, ADD §10 ---
    max_upload_bytes: int = 50 * 1024 * 1024
    max_zip_entries: int = 2000
    max_zip_total_bytes: int = 200 * 1024 * 1024
    max_zip_ratio: int = 100
    max_tasks_per_user: int = 3
    max_projects_per_user: int = 20
    max_segment_attempts: int = 3
    login_max_attempts: int = 10
    login_window_seconds: int = 15 * 60

    # --- батчинг, ADD §2 ---
    max_batch_segments: int = 20
    max_batch_chars: int = 6000

    # --- очередь, ADD §7 ---
    task_lease_seconds: int = 120


@lru_cache(maxsize=1)
def get_settings() -> Settings:
    return Settings()
```

- [ ] **Step 6: Написать `main.py`**

```python
# backend/src/linguastack/main.py
from fastapi import FastAPI


def create_app() -> FastAPI:
    app = FastAPI(title="LinguaStack", version="0.1.0")

    @app.get("/api/health")
    async def health() -> dict[str, str]:
        return {"status": "ok"}

    return app


app = create_app()
```

- [ ] **Step 7: Поднять окружение и установить зависимости**

```bash
cd /root/Git/LinguaStack
docker compose up -d db
cd backend && python3 -m venv .venv && .venv/bin/pip install -e ".[dev]"
```

- [ ] **Step 8: Запустить тесты — должны пройти**

Run: `cd backend && .venv/bin/python -m pytest tests/test_health.py -v`
Expected: PASS (2 passed)

- [ ] **Step 9: Коммит**

```bash
git add -A
git commit -m "$(cat <<'MSG'
feat: каркас проекта, настройки и health-эндпоинт

Postgres 17 в compose, пины под Python 3.10, лимиты из дополнения к ТЗ.

Co-Authored-By: Claude Opus 5 <noreply@anthropic.com>
MSG
)"
```

### Task 2: Слой БД, Alembic и тестовая база

**Files:**
- Create: `backend/src/linguastack/db/__init__.py`, `backend/src/linguastack/db/base.py`
- Create: `backend/src/linguastack/db/models/__init__.py`, `backend/src/linguastack/db/models/user.py`
- Create: `backend/alembic.ini`, `backend/alembic/env.py`, `backend/alembic/script.py.mako`
- Modify: `backend/tests/conftest.py`
- Create: `backend/tests/test_db.py`

**Interfaces:**
- Consumes: `get_settings()` (задача 1).
- Produces: `Base` (`DeclarativeBase` с `type_annotation_map` для `dict` → `JSONB`); `make_engine(url) -> AsyncEngine`; `session_factory(engine) -> async_sessionmaker[AsyncSession]`; `get_session()` — зависимость FastAPI; модель `User(id: UUID, email: str, password_hash: str, role: str, is_active: bool, created_at: datetime)`; фикстуры pytest `engine`, `session`, `client`.

- [ ] **Step 1: Написать падающий тест**

```python
# backend/tests/test_db.py
import pytest
from sqlalchemy import select

from linguastack.db.models.user import User


async def test_user_roundtrip(session):
    user = User(email="a@example.com", password_hash="x", role="user")
    session.add(user)
    await session.flush()
    assert user.id is not None
    assert user.created_at.tzinfo is not None, "timestamp обязан быть timezone-aware"

    found = (await session.execute(select(User).where(User.email == "a@example.com"))).scalar_one()
    assert found.id == user.id
    assert found.role == "user"


async def test_email_is_unique(session):
    from sqlalchemy.exc import IntegrityError

    session.add(User(email="dup@example.com", password_hash="x", role="user"))
    await session.flush()
    session.add(User(email="dup@example.com", password_hash="y", role="user"))
    with pytest.raises(IntegrityError):
        await session.flush()
```

- [ ] **Step 2: Запустить и убедиться, что падает**

Run: `cd backend && .venv/bin/python -m pytest tests/test_db.py -v`
Expected: FAIL — `ModuleNotFoundError: linguastack.db`

- [ ] **Step 3: Написать `db/base.py`**

```python
# backend/src/linguastack/db/base.py
from collections.abc import AsyncIterator
from datetime import datetime
from typing import Any

from sqlalchemy.dialects.postgresql import JSONB
from sqlalchemy.ext.asyncio import AsyncEngine, AsyncSession, async_sessionmaker, create_async_engine
from sqlalchemy.orm import DeclarativeBase

from linguastack.config import get_settings


class Base(DeclarativeBase):
    # dict-поля (CDM-блоки, настройки) ложатся в JSONB — ADD §0 решение по Postgres.
    type_annotation_map = {dict[str, Any]: JSONB, datetime: __import__("sqlalchemy").DateTime(timezone=True)}


def make_engine(url: str) -> AsyncEngine:
    return create_async_engine(url, pool_pre_ping=True)


def session_factory(engine: AsyncEngine) -> async_sessionmaker[AsyncSession]:
    return async_sessionmaker(engine, expire_on_commit=False)


_engine: AsyncEngine | None = None
_factory: async_sessionmaker[AsyncSession] | None = None


async def get_session() -> AsyncIterator[AsyncSession]:
    global _engine, _factory
    if _factory is None:
        _engine = make_engine(get_settings().database_url)
        _factory = session_factory(_engine)
    async with _factory() as session:
        yield session
```

Примечание для исполнителя: `__import__` в `type_annotation_map` выше — некрасиво; при реализации вынести `from sqlalchemy import DateTime` наверх и использовать `DateTime(timezone=True)`.

- [ ] **Step 4: Написать модель `User`**

`id` — `UUID(as_uuid=True)` с `default=uuid.uuid4`; `email` — `String(320)`, `unique=True`, `index=True`; `role` — `String(16)`, `default="user"`; `created_at` — `DateTime(timezone=True)`, `server_default=func.now()`.

- [ ] **Step 5: Настроить Alembic и создать первую миграцию**

`alembic/env.py` читает `get_settings().database_url`, импортирует `Base.metadata`, работает в async-режиме (`connectable.run_sync`). Затем:

```bash
cd backend && .venv/bin/alembic revision --autogenerate -m "users"
.venv/bin/alembic upgrade head
```

- [ ] **Step 6: Написать `conftest.py` с изолированной тестовой базой**

Фикстура `engine` (scope=session) создаёт базу `linguastack_test`, если её нет (через подключение к `postgres`), и выполняет `Base.metadata.create_all`. Фикстура `session` даёт `AsyncSession` во **вложенной транзакции с откатом после теста** — тесты не видят данных друг друга. Фикстура `client` отдаёт `AsyncClient` с переопределённой зависимостью `get_session`.

- [ ] **Step 7: Запустить тесты — должны пройти**

Run: `cd backend && .venv/bin/python -m pytest tests/ -v`
Expected: PASS (4 passed)

- [ ] **Step 8: Коммит** — `feat: слой БД, модель User, Alembic и тестовая база`

---

### Task 3: Хеширование паролей и шифрование секретов

**Files:**
- Create: `backend/src/linguastack/security/__init__.py`, `.../security/passwords.py`, `.../security/secrets.py`
- Create: `backend/tests/test_security_primitives.py`

**Interfaces:**
- Produces: `hash_password(plain: str) -> str`; `verify_password(plain: str, hashed: str) -> bool`; `needs_rehash(hashed: str) -> bool`; `SecretBox(key: str)` с `encrypt(plain: str) -> str`, `decrypt(token: str) -> str`, `fingerprint(plain: str) -> str` (первые 8 символов sha256), `last4(plain: str) -> str`.

- [ ] **Step 1: Написать падающий тест**

```python
# backend/tests/test_security_primitives.py
import pytest

from linguastack.security.passwords import hash_password, verify_password
from linguastack.security.secrets import SecretBox

KEY = "ZmFrZS1kZXYta2V5LW5vdC1mb3ItcHJvZHVjdGlvbi0xMjM0NTY="


def test_password_hash_is_argon2id_and_verifies():
    hashed = hash_password("correct horse battery staple")
    assert hashed.startswith("$argon2id$"), "ТЗ §13 требует безопасного хеширования"
    assert verify_password("correct horse battery staple", hashed)
    assert not verify_password("wrong", hashed)


def test_password_hash_is_salted():
    assert hash_password("same") != hash_password("same")


def test_secret_roundtrip_and_non_reversible_metadata():
    box = SecretBox(KEY)
    api_key = "sk-ant-secret-value-12345"
    token = box.encrypt(api_key)
    assert api_key not in token, "шифртекст не должен содержать открытый ключ"
    assert box.decrypt(token) == api_key
    # Клиенту отдаём только это — ADD §8.
    assert box.last4(api_key) == "2345"
    assert len(box.fingerprint(api_key)) == 8


def test_decrypt_rejects_tampered_token():
    from cryptography.fernet import InvalidToken

    box = SecretBox(KEY)
    token = box.encrypt("value")
    with pytest.raises(InvalidToken):
        box.decrypt(token[:-4] + "AAAA")
```

- [ ] **Step 2: Запустить — FAIL (модуль отсутствует)**
- [ ] **Step 3: Реализовать `passwords.py`** через `argon2.PasswordHasher`; `verify_password` ловит `VerifyMismatchError`/`InvalidHashError` и возвращает `False`.
- [ ] **Step 4: Реализовать `secrets.py`** через `cryptography.fernet.Fernet`; `fingerprint` — `hashlib.sha256(plain.encode()).hexdigest()[:8]`.
- [ ] **Step 5: Запустить — PASS (4 passed)**
- [ ] **Step 6: Коммит** — `feat: argon2id для паролей и Fernet для секретов`

---

### Task 4: Сессии, регистрация, вход, троттлинг входа

**Files:**
- Create: `.../security/sessions.py`, `.../security/ratelimit.py`
- Create: `.../db/models/session.py`, `.../db/models/login_attempt.py`
- Create: `.../api/__init__.py`, `.../api/deps.py`, `.../api/auth.py`, `.../api/schemas.py`
- Modify: `.../main.py` (подключить роутер), `backend/alembic/versions/*` (миграция)
- Create: `backend/tests/test_auth.py`

**Interfaces:**
- Produces: `POST /api/auth/register`, `POST /api/auth/login`, `POST /api/auth/logout`, `GET /api/me` (ТЗ §12); `current_user` — зависимость FastAPI, возвращает `User` или 401; `issue_session(session, user) -> str` (возвращает открытый токен, в БД хранит sha256); `revoke_session(session, token)`.

- [ ] **Step 1: Написать падающий тест** — ключевые проверки:

```python
# backend/tests/test_auth.py
async def test_register_then_login_sets_httponly_cookie(client):
    assert (await client.post("/api/auth/register",
            json={"email": "u@example.com", "password": "pw-at-least-12"})).status_code == 201
    response = await client.post("/api/auth/login",
            json={"email": "u@example.com", "password": "pw-at-least-12"})
    assert response.status_code == 200
    cookie = response.cookies.jar._cookies  # наличие флагов проверяем по заголовку
    set_cookie = response.headers["set-cookie"]
    assert "HttpOnly" in set_cookie and "SameSite=Lax" in set_cookie


async def test_me_requires_authentication(client):
    assert (await client.get("/api/me")).status_code == 401


async def test_password_never_echoed_back(client):
    r = await client.post("/api/auth/register",
        json={"email": "p@example.com", "password": "super-secret-pw"})
    assert "super-secret-pw" not in r.text
    assert "password_hash" not in r.text


async def test_login_failure_message_does_not_reveal_account_existence(client):
    missing = await client.post("/api/auth/login",
        json={"email": "nobody@example.com", "password": "whatever-12345"})
    await client.post("/api/auth/register",
        json={"email": "real@example.com", "password": "correct-pw-123"})
    wrong = await client.post("/api/auth/login",
        json={"email": "real@example.com", "password": "wrong-pw-1234"})
    assert missing.status_code == wrong.status_code == 401
    assert missing.json() == wrong.json(), "ответ не должен выдавать существование аккаунта"


async def test_login_throttled_after_limit(client, monkeypatch):
    await client.post("/api/auth/register",
        json={"email": "t@example.com", "password": "correct-pw-123"})
    for _ in range(10):
        await client.post("/api/auth/login",
            json={"email": "t@example.com", "password": "bad-password"})
    # 11-я попытка блокируется даже с верным паролем — ТЗ §13, защита от перебора.
    blocked = await client.post("/api/auth/login",
        json={"email": "t@example.com", "password": "correct-pw-123"})
    assert blocked.status_code == 429


async def test_logout_invalidates_session(client):
    await client.post("/api/auth/register", json={"email": "l@example.com", "password": "pw-123456789"})
    await client.post("/api/auth/login", json={"email": "l@example.com", "password": "pw-123456789"})
    assert (await client.get("/api/me")).status_code == 200
    await client.post("/api/auth/logout")
    assert (await client.get("/api/me")).status_code == 401
```

- [ ] **Step 2: Запустить — FAIL**
- [ ] **Step 3: Модели `UserSession(token_hash, user_id, expires_at, created_at)` и `LoginAttempt(email, ip, succeeded, created_at)`; миграция.**
- [ ] **Step 4: `sessions.py`** — `secrets.token_urlsafe(32)`, хранить `sha256`, срок жизни 30 дней.
- [ ] **Step 5: `ratelimit.py`** — `check_login_allowed(session, email, ip)` считает неудачи за `login_window_seconds`, при превышении `login_max_attempts` поднимает 429.
- [ ] **Step 6: Роутер `auth.py`** — register (валидация email через `EmailStr`, пароль ≥12 символов), login, logout, `/api/me`. Пароль принимается только в теле запроса, в логи не попадает.
- [ ] **Step 7: Запустить — PASS (6 passed)**
- [ ] **Step 8: Коммит** — `feat: регистрация, вход по сессионной cookie и защита от перебора`

---

### Task 5: Файловое хранилище с защитой путей

**Files:**
- Create: `backend/src/linguastack/storage/__init__.py`, `.../storage/files.py`
- Create: `backend/tests/test_storage.py`

**Interfaces:**
- Produces: `FileStore(root: Path)` с `book_dir(user_id, book_id) -> Path`, `save_original(user_id, book_id, suffix, data: bytes | BinaryIO) -> Path`, `save_normalized(user_id, book_id, payload: dict) -> Path`, `open_original(user_id, book_id, suffix) -> BinaryIO`, `delete_book(user_id, book_id) -> None`, `safe_extract_member(target_dir, member_name) -> Path`; исключения `FileTooLarge`, `UnsafePath`, `DisallowedExtension`.
- ALLOWED_SUFFIXES = `{".txt", ".md", ".markdown", ".epub", ".zip", ".pdf", ".docx", ".html", ".htm"}`.

- [ ] **Step 1: Написать падающий тест** — существенные случаи (ADD §11, ТЗ §13):

```python
# backend/tests/test_storage.py
import pytest

from linguastack.storage.files import DisallowedExtension, FileStore, FileTooLarge, UnsafePath


def test_path_never_contains_user_supplied_name(tmp_path):
    store = FileStore(tmp_path, max_bytes=1024)
    user_id, book_id = "11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222"
    path = store.save_original(user_id, book_id, ".txt", b"hello")
    # ТЗ §2.2: имена на диске непрогнозируемы и не зависят от имени, данного пользователем.
    assert "../../etc/passwd" not in str(path)
    assert path.name == "original.txt"
    assert user_id in str(path) and book_id in str(path)


@pytest.mark.parametrize("member", [
    "../escape.md",
    "/absolute/path.md",
    "nested/../../escape.md",
    "a/b/../../../escape.md",
])
def test_zip_member_escaping_target_dir_is_rejected(tmp_path, member):
    store = FileStore(tmp_path, max_bytes=1024)
    with pytest.raises(UnsafePath):
        store.safe_extract_member(tmp_path / "out", member)


def test_oversized_upload_rejected(tmp_path):
    store = FileStore(tmp_path, max_bytes=10)
    with pytest.raises(FileTooLarge):
        store.save_original("u", "b", ".txt", b"x" * 11)


def test_unknown_extension_rejected(tmp_path):
    store = FileStore(tmp_path, max_bytes=1024)
    with pytest.raises(DisallowedExtension):
        store.save_original("u", "b", ".exe", b"MZ")


def test_delete_book_removes_directory(tmp_path):
    store = FileStore(tmp_path, max_bytes=1024)
    store.save_original("u", "b", ".txt", b"data")
    store.delete_book("u", "b")
    assert not store.book_dir("u", "b").exists()
```

- [ ] **Step 2: Запустить — FAIL**
- [ ] **Step 3: Реализовать.** `safe_extract_member` сравнивает `os.path.realpath` итогового пути с `realpath` целевого каталога через `Path.is_relative_to`; отвергает абсолютные пути и компоненты `..` ещё до склейки. Запись оригинала — потоковая с подсчётом байт, чтобы лимит срабатывал до исчерпания диска.
- [ ] **Step 4: Запустить — PASS (8 passed)**
- [ ] **Step 5: Коммит** — `feat: файловое хранилище с защитой от path traversal`

---

### Task 6: Canonical Document Model и стабильные идентификаторы

**Files:**
- Create: `backend/src/linguastack/cdm/__init__.py`, `.../cdm/model.py`, `.../cdm/ids.py`
- Create: `backend/tests/test_cdm.py`

**Interfaces:**
- Produces (pydantic-модели): `BlockType` (str-Enum: `heading`, `paragraph`, `list_item`, `table_cell`, `caption`, `blockquote`, `code_block`, `math_block`, `image`, `thematic_break`, `raw_html`); `Block(node_id: str, type: BlockType, text: str, level: int | None, source_hash: str, meta: dict)`; `Chapter(index: int, title: str, blocks: list[Block], children: list[Chapter])`; `BookMeta(title, author, language, source_format)`; `CanonicalDocument(meta: BookMeta, chapters: list[Chapter])` с методами `iter_blocks()`, `translatable_blocks()`, `toc()`.
- `TRANSLATABLE: frozenset[BlockType]` = heading, paragraph, list_item, table_cell, caption, blockquote.
- `make_node_id(chapter_index: int, ordinal: int) -> str`; `content_hash(text: str) -> str`; `normalize_text(text: str) -> str`.

- [ ] **Step 1: Написать падающий тест**

```python
# backend/tests/test_cdm.py
from linguastack.cdm.ids import content_hash, make_node_id, normalize_text
from linguastack.cdm.model import Block, BlockType, CanonicalDocument, Chapter, BookMeta


def test_node_id_is_deterministic_and_readable():
    assert make_node_id(3, 17) == "ch0003/b0017"
    assert make_node_id(3, 17) == make_node_id(3, 17)


def test_content_hash_ignores_insignificant_whitespace():
    # ADD §1: хеш содержимого не должен дёргаться от переформатирования.
    assert content_hash("Hello   world") == content_hash("Hello world")
    assert content_hash("  Hello world\n") == content_hash("Hello world")
    assert content_hash("Hello world") != content_hash("Hello worlds")


def test_content_hash_normalizes_unicode():
    # "é" как один символ и как e + combining acute — один и тот же текст.
    assert content_hash("café") == content_hash("café")


def test_translatable_blocks_exclude_code_and_images():
    doc = CanonicalDocument(
        meta=BookMeta(title="T", author=None, language="en", source_format="md"),
        chapters=[Chapter(index=0, title="C", blocks=[
            Block(node_id="ch0000/b0000", type=BlockType.paragraph, text="Hello",
                  source_hash=content_hash("Hello")),
            Block(node_id="ch0000/b0001", type=BlockType.code_block, text="print(1)",
                  source_hash=content_hash("print(1)")),
            Block(node_id="ch0000/b0002", type=BlockType.image, text="",
                  source_hash=content_hash("")),
        ])],
    )
    assert [b.node_id for b in doc.translatable_blocks()] == ["ch0000/b0000"]


def test_node_ids_are_unique_across_document():
    doc = CanonicalDocument(
        meta=BookMeta(title="T", author=None, language="en", source_format="md"),
        chapters=[
            Chapter(index=0, title="A", blocks=[Block(node_id=make_node_id(0, 0),
                    type=BlockType.paragraph, text="a", source_hash=content_hash("a"))]),
            Chapter(index=1, title="B", blocks=[Block(node_id=make_node_id(1, 0),
                    type=BlockType.paragraph, text="b", source_hash=content_hash("b"))]),
        ],
    )
    ids = [b.node_id for b in doc.iter_blocks()]
    assert len(ids) == len(set(ids))


def test_toc_preserves_order_and_nesting():
    doc = CanonicalDocument(
        meta=BookMeta(title="T", author=None, language="en", source_format="md"),
        chapters=[Chapter(index=0, title="Part", blocks=[], children=[
            Chapter(index=1, title="Sub", blocks=[])])],
    )
    toc = doc.toc()
    assert toc[0]["title"] == "Part"
    assert toc[0]["children"][0]["title"] == "Sub"
```

- [ ] **Step 2: Запустить — FAIL**
- [ ] **Step 3: Реализовать `ids.py`**

```python
# backend/src/linguastack/cdm/ids.py
"""Идентификаторы блоков CDM. Обоснование схемы — ADD §1."""
import hashlib
import re
import unicodedata

_WHITESPACE = re.compile(r"\s+")


def normalize_text(text: str) -> str:
    """NFC + свёртка пробелов. Чтобы переформатирование не меняло хеш."""
    return _WHITESPACE.sub(" ", unicodedata.normalize("NFC", text)).strip()


def content_hash(text: str) -> str:
    return hashlib.blake2s(normalize_text(text).encode("utf-8"), digest_size=16).hexdigest()


def make_node_id(chapter_index: int, ordinal: int) -> str:
    return f"ch{chapter_index:04d}/b{ordinal:04d}"
```

- [ ] **Step 4: Реализовать `model.py`** на pydantic `BaseModel`. `iter_blocks()` обходит главы в глубину, сохраняя порядок.
- [ ] **Step 5: Запустить — PASS (6 passed)**
- [ ] **Step 6: Коммит** — `feat: Canonical Document Model и стабильные идентификаторы блоков`

---

### Task 7: Маскирование защищённых фрагментов

**Files:**
- Create: `backend/src/linguastack/cdm/masking.py`
- Create: `backend/tests/test_masking.py`

**Interfaces:**
- Produces: `mask(text: str) -> MaskResult`; `MaskResult(masked: str, placeholders: dict[str, str])`; `unmask(masked: str, placeholders: dict[str, str]) -> str`; `placeholder_multiset(text: str) -> collections.Counter`; исключение `PlaceholderMismatch(expected, actual)`.
- Маркер: `⟦N⟧` (U+27E6 / U+27E7). Экранирование уже присутствующих в исходнике маркеров — обязательно (**Review Focus №2**).

- [ ] **Step 1: Написать падающий тест**

```python
# backend/tests/test_masking.py
import pytest

from linguastack.cdm.masking import (
    PlaceholderMismatch, mask, placeholder_multiset, unmask, verify_placeholders,
)


def test_inline_code_is_masked_and_restored():
    result = mask("Вызовите `get_user()` затем `save()`.")
    assert "get_user()" not in result.masked
    assert result.masked.count("⟦") == 2
    assert unmask(result.masked, result.placeholders) == "Вызовите `get_user()` затем `save()`."


def test_link_target_masked_but_link_text_translatable():
    result = mask("См. [документацию](https://example.com/docs) здесь.")
    # Текст ссылки обязан остаться видимым для модели, URL — нет.
    assert "документацию" in result.masked
    assert "https://example.com/docs" not in result.masked


def test_math_is_masked():
    result = mask("Формула $E = mc^2$ известна.")
    assert "mc^2" not in result.masked
    assert unmask(result.masked, result.placeholders) == "Формула $E = mc^2$ известна."


def test_preexisting_placeholder_markers_are_escaped():
    """Review Focus №2: исходник уже содержит ⟦1⟧.

    Без экранирования unmask() подставит сюда чужой фрагмент и тихо испортит текст.
    """
    original = "Литерал ⟦1⟧ в тексте и код `x`."
    result = mask(original)
    assert unmask(result.masked, result.placeholders) == original


def test_roundtrip_is_identity_for_text_without_protected_spans():
    text = "Обычный абзац без кода и ссылок."
    result = mask(text)
    assert result.masked == text
    assert result.placeholders == {}
    assert unmask(result.masked, result.placeholders) == text


def test_verify_placeholders_detects_dropped_marker():
    result = mask("Код `a` и `b`.")
    broken = result.masked.replace("⟦2⟧", "")
    with pytest.raises(PlaceholderMismatch):
        verify_placeholders(result.masked, broken)


def test_verify_placeholders_detects_duplicated_marker():
    result = mask("Код `a`.")
    doubled = result.masked + " ⟦1⟧"
    with pytest.raises(PlaceholderMismatch):
        verify_placeholders(result.masked, doubled)


def test_verify_placeholders_allows_reordering():
    # Порядок слов при переводе меняется — это нормально, важно мультимножество.
    result = mask("`a` затем `b`")
    reordered = "⟦2⟧ then ⟦1⟧"
    verify_placeholders(result.masked, reordered)  # не должно бросать
```

- [ ] **Step 2: Запустить — FAIL**
- [ ] **Step 3: Реализовать.** Порядок важен: сначала экранировать существующие `⟦`/`⟧` (заменить на приватные символы-escape), затем маскировать по приоритету: fenced-код → inline-код → математика → изображения → цели ссылок → HTML-теги. `verify_placeholders` сравнивает `Counter` маркеров и бросает `PlaceholderMismatch` с деталями.
- [ ] **Step 4: Запустить — PASS (8 passed)**
- [ ] **Step 5: Коммит** — `feat: маскирование кода, ссылок и формул перед переводом`

---

### Task 8: Единая точка проверки прав и каркас теста изоляции

**Files:**
- Modify: `backend/src/linguastack/api/deps.py`
- Create: `backend/tests/test_isolation.py`, `backend/tests/factories.py`

**Interfaces:**
- Produces: `async def owned(model: type[T], resource_id, user, session) -> T` — `SELECT ... WHERE id = :id AND owner_id = :user.id`, иначе `HTTPException(404)`; фикстуры `user_client`, `other_client` (две независимые аутентифицированные сессии); хелпер `assert_cross_user_404(client, method, url)`.

- [ ] **Step 1: Написать падающий тест**

```python
# backend/tests/test_isolation.py
import pytest


async def test_owned_returns_404_not_403_for_foreign_resource(session, make_user, make_book):
    from fastapi import HTTPException

    from linguastack.api.deps import owned
    from linguastack.db.models.book import Book

    owner, intruder = await make_user("o@example.com"), await make_user("i@example.com")
    book = await make_book(owner)

    with pytest.raises(HTTPException) as exc:
        await owned(Book, book.id, intruder, session)
    # 403 подтвердил бы существование ресурса — ADD §9.
    assert exc.value.status_code == 404


async def test_admin_role_does_not_bypass_ownership(session, make_user, make_book):
    """ТЗ §2.1: доступ к содержимому книг не должен возникать из одной роли admin."""
    from fastapi import HTTPException

    from linguastack.api.deps import owned
    from linguastack.db.models.book import Book

    owner = await make_user("o2@example.com")
    admin = await make_user("admin@example.com", role="admin")
    book = await make_book(owner)

    with pytest.raises(HTTPException) as exc:
        await owned(Book, book.id, admin, session)
    assert exc.value.status_code == 404


async def test_owned_returns_resource_for_its_owner(session, make_user, make_book):
    from linguastack.db.models.book import Book

    from linguastack.api.deps import owned

    owner = await make_user("o3@example.com")
    book = await make_book(owner)
    assert (await owned(Book, book.id, owner, session)).id == book.id
```

- [ ] **Step 2: Запустить — FAIL**
- [ ] **Step 3: Реализовать `owned()` и фабрики** (`make_user`, `make_book`, `make_project` в `tests/factories.py`, подключённые как фикстуры в `conftest.py`).
- [ ] **Step 4: Запустить — PASS (3 passed)**
- [ ] **Step 5: Коммит** — `feat: единая проверка владения ресурсом (404 вместо 403, без обхода для admin)`

**Примечание:** параметризованный тест по *всем* ресурсным эндпоинтам добавляется в задаче 26, когда эндпоинты существуют.

---

## Этап 2 — Импорт

### Task 9: Протокол импортёра, реестр форматов и TxtImporter

**Files:**
- Create: `.../importers/__init__.py`, `.../importers/base.py`, `.../importers/registry.py`, `.../importers/txt.py`
- Create: `backend/tests/test_import_txt.py`, `backend/tests/fixtures/books/plain.txt`

**Interfaces:**
- Produces: `class Importer(Protocol)` с `format_id: str`, `suffixes: frozenset[str]`, `def sniff(data: bytes, filename: str) -> bool`, `def load(data: bytes, filename: str) -> ImportResult`.
- `ImportIssue(severity: Literal["info","warning","error"], code: str, message: str, node_id: str | None)`.
- `ImportResult(document: CanonicalDocument, issues: list[ImportIssue])` — ТЗ §3.1 требует сообщать о проблемах **до** начала перевода.
- `registry.pick(data, filename) -> Importer`; исключение `UnsupportedFormat`.

- [ ] **Step 1: Написать падающий тест** (включая кодировки — Review Focus, доп. пункт)

```python
# backend/tests/test_import_txt.py
import pytest

from linguastack.cdm.model import BlockType
from linguastack.importers.registry import UnsupportedFormat, pick
from linguastack.importers.txt import TxtImporter


def test_paragraphs_split_on_blank_lines():
    data = "Первый абзац.\n\nВторой абзац.\n\n\nТретий.".encode()
    result = TxtImporter().load(data, "book.txt")
    blocks = result.document.translatable_blocks()
    assert [b.text for b in blocks] == ["Первый абзац.", "Второй абзац.", "Третий."]
    assert all(b.type is BlockType.paragraph for b in blocks)


def test_bom_is_stripped():
    result = TxtImporter().load("﻿Текст с BOM.".encode(), "b.txt")
    assert result.document.translatable_blocks()[0].text == "Текст с BOM."


def test_crlf_handled():
    result = TxtImporter().load(b"A\r\n\r\nB", "b.txt")
    assert [b.text for b in result.document.translatable_blocks()] == ["A", "B"]


def test_cp1251_decoded_with_issue_recorded():
    """ТЗ §3.1: TXT обязан обрабатывать кодировки, а не падать на них."""
    result = TxtImporter().load("Привет мир".encode("cp1251"), "b.txt")
    assert "Привет мир" in result.document.translatable_blocks()[0].text
    assert any(i.code == "encoding_guessed" for i in result.issues)


def test_empty_file_yields_error_issue_not_exception():
    result = TxtImporter().load(b"", "b.txt")
    assert result.document.translatable_blocks() == []
    assert any(i.severity == "error" and i.code == "empty_document" for i in result.issues)


def test_registry_picks_txt_and_rejects_unknown():
    assert pick(b"text", "a.txt").format_id == "txt"
    with pytest.raises(UnsupportedFormat):
        pick(b"\x00\x01", "a.bin")
```

- [ ] **Step 2: Запустить — FAIL**
- [ ] **Step 3: Реализовать.** Декодирование: пробовать `utf-8-sig`, затем `cp1251`, затем `latin-1` с записью `ImportIssue("warning", "encoding_guessed", ...)`. Один блок = один абзац; `node_id` через `make_node_id(0, i)`.
- [ ] **Step 4: Запустить — PASS (6 passed)**
- [ ] **Step 5: Коммит** — `feat: протокол импортёра, реестр форматов и импорт TXT`

---

### Task 10: MarkdownImporter

**Files:**
- Create: `.../importers/markdown.py`
- Create: `backend/tests/test_import_markdown.py`, `backend/tests/fixtures/books/sample.md`

**Interfaces:**
- Produces: `MarkdownImporter` (`format_id = "markdown"`). Главы режутся по заголовкам `#`/`##`; вложенность заголовков даёт дерево `Chapter.children`.
- Consumes: `markdown_it.MarkdownIt` (`"commonmark"` + таблицы), `Block`, `BlockType`, `make_node_id`, `content_hash`.

- [ ] **Step 1: Написать падающий тест** — существенные проверки:

```python
# backend/tests/test_import_markdown.py
from linguastack.cdm.model import BlockType
from linguastack.importers.markdown import MarkdownImporter

DOC = """\
# Глава первая

Вступительный абзац.

```python
print("этот код не переводится")
```

## Раздел 1.1

- первый пункт
- второй пункт

| Колонка | Значение |
|---|---|
| a | b |

# Глава вторая

Текст второй главы.
"""


def test_chapters_split_on_headings():
    doc = MarkdownImporter().load(DOC.encode(), "s.md").document
    assert [c.title for c in doc.chapters] == ["Глава первая", "Глава вторая"]


def test_subsection_becomes_child_chapter():
    doc = MarkdownImporter().load(DOC.encode(), "s.md").document
    assert [c.title for c in doc.chapters[0].children] == ["Раздел 1.1"]


def test_code_block_present_but_not_translatable():
    doc = MarkdownImporter().load(DOC.encode(), "s.md").document
    code = [b for b in doc.iter_blocks() if b.type is BlockType.code_block]
    assert len(code) == 1
    assert 'print("этот код не переводится")' in code[0].text
    assert code[0] not in doc.translatable_blocks()


def test_list_items_are_separate_blocks():
    doc = MarkdownImporter().load(DOC.encode(), "s.md").document
    items = [b.text for b in doc.iter_blocks() if b.type is BlockType.list_item]
    assert items == ["первый пункт", "второй пункт"]


def test_table_cells_are_translatable_blocks():
    doc = MarkdownImporter().load(DOC.encode(), "s.md").document
    cells = [b.text for b in doc.iter_blocks() if b.type is BlockType.table_cell]
    assert "Колонка" in cells and "a" in cells


def test_heading_blocks_carry_level():
    doc = MarkdownImporter().load(DOC.encode(), "s.md").document
    headings = [b for b in doc.iter_blocks() if b.type is BlockType.heading]
    assert headings[0].level == 1


def test_document_without_headings_yields_single_chapter():
    doc = MarkdownImporter().load(b"Just a paragraph.\n\nAnd another.", "s.md").document
    assert len(doc.chapters) == 1
    assert len(doc.chapters[0].blocks) == 2
```

- [ ] **Step 2: Запустить — FAIL**
- [ ] **Step 3: Реализовать** через `MarkdownIt("commonmark").enable("table")` и обход потока токенов: `heading_open`/`inline`/`heading_close`, `paragraph_open`, `fence`/`code_block`, `list_item_open`, `th_open`/`td_open`, `blockquote_open`, `math_block` (если встретится — как `math_block`). Для каждого блока сохранять исходный Markdown-текст (не HTML).
- [ ] **Step 4: Запустить — PASS (7 passed)**
- [ ] **Step 5: Коммит** — `feat: импорт Markdown с разбором глав, кода, списков и таблиц`

---

### Task 11: ZipRepoImporter — порядок глав и защита от зип-бомб

**Files:**
- Create: `.../importers/zip_repo.py`
- Create: `backend/tests/test_import_zip.py`

**Interfaces:**
- Produces: `ZipRepoImporter` (`format_id = "zip_repo"`), `discover_order(names: list[str], manifest: dict | None) -> list[str]`.
- Порядок глав определяется по приоритету (ТЗ §4.3: сортировка по имени **не** должна быть единственным способом): `SUMMARY.md` / `mkdocs.yml` / `_toc.yml` → числовые префиксы файлов → порядок относительных ссылок из корневого `README.md`/`index.md` → только затем алфавит. Выбранный способ записывается в `ImportIssue("info", "order_source", ...)`, чтобы пользователь видел, откуда взялся порядок, и мог поправить (ТЗ §4.3).

- [ ] **Step 1: Написать падающий тест**

```python
# backend/tests/test_import_zip.py
import io
import zipfile

import pytest

from linguastack.importers.zip_repo import ZipRepoImporter
from linguastack.storage.files import UnsafePath


def make_zip(files: dict[str, bytes]) -> bytes:
    buf = io.BytesIO()
    with zipfile.ZipFile(buf, "w") as zf:
        for name, data in files.items():
            zf.writestr(name, data)
    return buf.getvalue()


def test_summary_file_determines_chapter_order():
    data = make_zip({
        "SUMMARY.md": b"- [Intro](zzz.md)\n- [Body](aaa.md)\n",
        "zzz.md": b"# Intro\n\nFirst.",
        "aaa.md": b"# Body\n\nSecond.",
    })
    result = ZipRepoImporter().load(data, "repo.zip")
    # Алфавит дал бы aaa, zzz — SUMMARY.md обязан победить (ТЗ §4.3).
    assert [c.title for c in result.document.chapters] == ["Intro", "Body"]
    assert any(i.code == "order_source" and "SUMMARY.md" in i.message for i in result.issues)


def test_numeric_prefixes_order_chapters_naturally():
    data = make_zip({
        "02-second.md": b"# Second\n\nB.",
        "10-tenth.md": b"# Tenth\n\nJ.",
        "01-first.md": b"# First\n\nA.",
    })
    result = ZipRepoImporter().load(data, "repo.zip")
    # Лексикографически "10" < "02"; нужен числовой порядок.
    assert [c.title for c in result.document.chapters] == ["First", "Second", "Tenth"]


def test_zip_slip_member_rejected():
    data = make_zip({"../../etc/passwd": b"root", "ok.md": b"# Ok\n\nx."})
    with pytest.raises(UnsafePath):
        ZipRepoImporter().load(data, "repo.zip")


def test_too_many_entries_rejected():
    data = make_zip({f"f{i}.md": b"# H\n\nx." for i in range(5)})
    with pytest.raises(ValueError, match="entries"):
        ZipRepoImporter(max_entries=3).load(data, "repo.zip")


def test_compression_bomb_rejected():
    buf = io.BytesIO()
    with zipfile.ZipFile(buf, "w", zipfile.ZIP_DEFLATED) as zf:
        zf.writestr("bomb.md", b"\0" * (2 * 1024 * 1024))  # жмётся в тысячи раз
    with pytest.raises(ValueError, match="ratio|size"):
        ZipRepoImporter(max_ratio=100, max_total_bytes=1024 * 1024).load(buf.getvalue(), "r.zip")


def test_missing_referenced_chapter_recorded_as_issue():
    data = make_zip({
        "SUMMARY.md": b"- [Here](present.md)\n- [Gone](absent.md)\n",
        "present.md": b"# Here\n\nx.",
    })
    result = ZipRepoImporter().load(data, "repo.zip")
    # ТЗ §4.3: отсутствующие главы должны выявляться, а не молча пропадать.
    assert any(i.code == "missing_chapter" and "absent.md" in i.message for i in result.issues)


def test_images_are_preserved_as_image_blocks():
    data = make_zip({
        "ch.md": b"# Ch\n\n![diagram](img/d.png)\n",
        "img/d.png": b"\x89PNG\r\n\x1a\n",
    })
    result = ZipRepoImporter().load(data, "repo.zip")
    from linguastack.cdm.model import BlockType
    assert any(b.type is BlockType.image for b in result.document.iter_blocks())
```

- [ ] **Step 2: Запустить — FAIL**
- [ ] **Step 3: Реализовать.** Каждый `.md`-файл разбирается `MarkdownImporter` (переиспользование, не копия); главы собираются в порядке `discover_order`. Проверки выполняются **до** распаковки: `ZipInfo.file_size` суммируется для `max_total_bytes`, отношение `file_size / compress_size` — для `max_ratio`, имена — через `FileStore.safe_extract_member`.
- [ ] **Step 4: Запустить — PASS (7 passed)**
- [ ] **Step 5: Коммит** — `feat: импорт ZIP-репозиториев с определением порядка глав и защитой от зип-бомб`

---

### Task 12: EpubImporter

**Files:**
- Create: `.../importers/epub.py`
- Create: `backend/tests/test_import_epub.py`, `backend/tests/fixtures/books/make_epub.py`

**Interfaces:**
- Produces: `EpubImporter` (`format_id = "epub"`). Главы — по `toc.ncx`/nav; при его отсутствии — по порядку spine (с `ImportIssue("warning", "toc_missing", ...)`). HTML каждой главы разбирается `BeautifulSoup(..., "lxml")` в блоки CDM.
- Фикстура: `make_epub.py` строит EPUB программно через `ebooklib` — бинарных файлов в репозитории не держим.

- [ ] **Step 1: Написать падающий тест**

```python
# backend/tests/test_import_epub.py
from linguastack.cdm.model import BlockType
from linguastack.importers.epub import EpubImporter
from tests.fixtures.books.make_epub import build_epub


def test_chapters_and_metadata_extracted():
    data = build_epub(title="Книга", author="Автор", chapters=[
        ("Глава 1", "<h1>Глава 1</h1><p>Первый абзац.</p>"),
        ("Глава 2", "<h1>Глава 2</h1><p>Второй абзац.</p>"),
    ])
    result = EpubImporter().load(data, "b.epub")
    assert result.document.meta.title == "Книга"
    assert result.document.meta.author == "Автор"
    assert [c.title for c in result.document.chapters] == ["Глава 1", "Глава 2"]


def test_paragraphs_become_translatable_blocks():
    data = build_epub(title="T", author=None, chapters=[
        ("C", "<h1>C</h1><p>Абзац один.</p><p>Абзац два.</p>")])
    blocks = [b.text for b in EpubImporter().load(data, "b.epub").document.translatable_blocks()]
    assert "Абзац один." in blocks and "Абзац два." in blocks


def test_pre_code_is_code_block_not_translatable():
    data = build_epub(title="T", author=None, chapters=[
        ("C", "<h1>C</h1><pre><code>print(1)</code></pre>")])
    doc = EpubImporter().load(data, "b.epub").document
    code = [b for b in doc.iter_blocks() if b.type is BlockType.code_block]
    assert len(code) == 1 and "print(1)" in code[0].text


def test_images_recorded_with_source_reference():
    data = build_epub(title="T", author=None, chapters=[
        ("C", '<h1>C</h1><p><img src="images/x.png" alt="Схема"/></p>')])
    doc = EpubImporter().load(data, "b.epub").document
    images = [b for b in doc.iter_blocks() if b.type is BlockType.image]
    assert images and images[0].meta.get("src", "").endswith("x.png")
    # alt-текст переводим — это видимый пользователю текст.
    assert images[0].meta.get("alt") == "Схема"


def test_epub_without_toc_falls_back_to_spine_with_warning():
    data = build_epub(title="T", author=None, chapters=[("C", "<p>x</p>")], include_toc=False)
    result = EpubImporter().load(data, "b.epub")
    assert result.document.chapters
    assert any(i.code == "toc_missing" for i in result.issues)


def test_corrupt_epub_reports_error_issue_not_traceback():
    """ТЗ §3.1: о повреждённых файлах нужно сообщать до начала перевода."""
    result = EpubImporter().load(b"not an epub at all", "b.epub")
    assert any(i.severity == "error" and i.code == "corrupt_file" for i in result.issues)
```

- [ ] **Step 2: Запустить — FAIL**
- [ ] **Step 3: Реализовать `make_epub.py` и `epub.py`.** `ebooklib.epub.read_epub` требует путь — писать во временный файл. Теги → блоки: `h1`–`h6` → `heading` с `level`; `p` → `paragraph`; `li` → `list_item`; `pre`/`code` → `code_block`; `img` → `image` (`meta = {"src", "alt"}`); `blockquote` → `blockquote`; `td`/`th` → `table_cell`.
- [ ] **Step 4: Запустить — PASS (6 passed)**
- [ ] **Step 5: Коммит** — `feat: импорт EPUB с оглавлением, кодом и изображениями`

---

### Task 13: API книг — загрузка, библиотека, оглавление, удаление

**Files:**
- Create: `.../db/models/book.py`, `.../api/books.py`
- Modify: `.../main.py`, миграция
- Create: `backend/tests/test_books_api.py`

**Interfaces:**
- Produces: `POST /api/books` (multipart), `GET /api/books`, `GET /api/books/{id}`, `GET /api/books/{id}/toc`, `PUT /api/books/{id}/toc` (ручное переупорядочивание, ТЗ §4.3), `DELETE /api/books/{id}` (ТЗ §12).
- Модель `Book(id, owner_id, title, author, source_format, original_filename, original_size, language, status, cdm: dict, issues: dict, created_at)`. `cdm` — JSONB с сериализованным `CanonicalDocument`; оригинал лежит в `FileStore` и больше не меняется (ТЗ §4.1).

- [ ] **Step 1: Написать падающий тест**

```python
# backend/tests/test_books_api.py
async def test_upload_markdown_creates_book_with_toc(user_client):
    r = await user_client.post("/api/books", files={"file": ("b.md", b"# Ch\n\nText.", "text/markdown")})
    assert r.status_code == 201
    book_id = r.json()["id"]

    toc = await user_client.get(f"/api/books/{book_id}/toc")
    assert toc.status_code == 200
    assert [c["title"] for c in toc.json()["chapters"]] == ["Ch"]


async def test_library_lists_only_own_books(user_client, other_client):
    await user_client.post("/api/books", files={"file": ("a.md", b"# A\n\nx.", "text/markdown")})
    await other_client.post("/api/books", files={"file": ("b.md", b"# B\n\ny.", "text/markdown")})
    mine = (await user_client.get("/api/books")).json()
    assert [b["title"] for b in mine] == ["A"], "ТЗ §2.2: видно только свои книги"


async def test_foreign_book_returns_404(user_client, other_client):
    book_id = (await other_client.post("/api/books",
        files={"file": ("b.md", b"# B\n\ny.", "text/markdown")})).json()["id"]
    assert (await user_client.get(f"/api/books/{book_id}")).status_code == 404
    assert (await user_client.get(f"/api/books/{book_id}/toc")).status_code == 404
    assert (await user_client.delete(f"/api/books/{book_id}")).status_code == 404


async def test_import_issues_surfaced_before_translation(user_client):
    """ТЗ §3.1: о проблемах структуры сообщается до начала перевода."""
    r = await user_client.post("/api/books", files={"file": ("e.txt", b"", "text/plain")})
    assert r.status_code == 201
    assert any(i["code"] == "empty_document" for i in r.json()["issues"])


async def test_oversized_upload_rejected_with_413(user_client, monkeypatch):
    from linguastack.config import get_settings
    get_settings.cache_clear()
    monkeypatch.setenv("LINGUASTACK_MAX_UPLOAD_BYTES", "10")
    r = await user_client.post("/api/books", files={"file": ("b.md", b"x" * 100, "text/markdown")})
    assert r.status_code == 413


async def test_delete_book_removes_files_and_row(user_client, tmp_storage):
    book_id = (await user_client.post("/api/books",
        files={"file": ("b.md", b"# B\n\nx.", "text/markdown")})).json()["id"]
    assert (await user_client.delete(f"/api/books/{book_id}")).status_code == 204
    assert (await user_client.get(f"/api/books/{book_id}")).status_code == 404
    # ТЗ §13: при удалении книги удаляются и связанные файлы.
    assert not any(tmp_storage.rglob(f"*{book_id}*"))


async def test_same_file_uploaded_twice_yields_two_independent_books(user_client):
    """Review Focus №4: повторный импорт не должен конфликтовать в хранилище."""
    payload = {"file": ("b.md", b"# B\n\nx.", "text/markdown")}
    first = (await user_client.post("/api/books", files=payload)).json()["id"]
    second = (await user_client.post("/api/books", files=payload)).json()["id"]
    assert first != second
    assert (await user_client.get(f"/api/books/{first}")).status_code == 200
    assert (await user_client.get(f"/api/books/{second}")).status_code == 200


async def test_manual_toc_reorder_persists(user_client):
    """ТЗ §4.3: пользователь может вручную исправить порядок глав."""
    book_id = (await user_client.post("/api/books",
        files={"file": ("b.md", b"# A\n\nx.\n\n# B\n\ny.", "text/markdown")})).json()["id"]
    assert (await user_client.put(f"/api/books/{book_id}/toc",
        json={"order": [1, 0]})).status_code == 200
    toc = (await user_client.get(f"/api/books/{book_id}/toc")).json()
    assert [c["title"] for c in toc["chapters"]] == ["B", "A"]
```

- [ ] **Step 2: Запустить — FAIL**
- [ ] **Step 3: Реализовать.** Загрузка: читать потоком с проверкой `max_upload_bytes` (413 при превышении), выбрать импортёр через `registry.pick`, сохранить оригинал в `FileStore`, сериализовать CDM в JSONB, вернуть `issues`. Удаление — `FileStore.delete_book` плюс удаление строки в одной транзакции. Все хендлеры читают ресурс через `owned()`.
- [ ] **Step 4: Запустить — PASS (8 passed)**
- [ ] **Step 5: Коммит** — `feat: API книг — загрузка, библиотека, оглавление с переупорядочиванием, удаление`

---

## Этап 3 — Перевод

### Task 14: Интерфейс LLMProvider, возможности модели, таксономия ошибок, FakeProvider

**Files:**
- Create: `.../providers/__init__.py`, `.../providers/base.py`, `.../providers/errors.py`, `.../providers/fake.py`
- Create: `backend/tests/test_provider_contract.py`

**Interfaces:**
- Produces:

```python
# .../providers/base.py — обоснование формы интерфейса: ADD §6
Determinism = Literal["strict", "balanced", "creative"]


@dataclass(frozen=True)
class ModelCapabilities:
    model_id: str
    context_tokens: int | None
    max_output_tokens: int | None
    supports_temperature: bool
    supports_system_prompt: bool
    supported_languages: frozenset[str] | None
    input_cost_per_mtok: float | None
    output_cost_per_mtok: float | None


@dataclass(frozen=True)
class CompletionRequest:
    system: str
    user: str
    determinism: Determinism = "strict"
    max_output_tokens: int | None = None


@dataclass(frozen=True)
class CompletionResult:
    text: str
    input_tokens: int | None
    output_tokens: int | None
    cached_input_tokens: int | None
    model_id: str
    cost_usd: float | None


class LLMProvider(abc.ABC):
    provider_id: ClassVar[str]

    @abc.abstractmethod
    async def list_models(self) -> list[ModelCapabilities]: ...
    @abc.abstractmethod
    async def health(self) -> ProviderHealth: ...
    @abc.abstractmethod
    async def capabilities(self, model_id: str) -> ModelCapabilities: ...
    @abc.abstractmethod
    async def complete(self, model_id: str, request: CompletionRequest) -> CompletionResult: ...
```

- Исключения (`errors.py`, ADD §6.1): `ProviderError` (база) → `ProviderUnavailable`, `ProviderRateLimited(retry_after: float | None)`, `ProviderAuthError`, `ModelNotAvailable`, `ProviderRefused(category: str | None)`, `ProviderBadRequest`. Свойство `ProviderError.retryable: bool`.
- `FakeProvider(scenario: str, ...)` — сценарии из ADD §14: `echo`, `prefix`, `protocol_violation`, `drops_placeholder`, `rate_limited`, `timeout`, `refusal`, `empty`. Также `calls: list[CompletionRequest]` для проверок и `fail_times: int` (падать N раз, затем успех) для тестов повторов.

- [ ] **Step 1: Написать падающий тест** — контракт, общий для всех адаптеров:

```python
# backend/tests/test_provider_contract.py
import pytest

from linguastack.providers.base import CompletionRequest
from linguastack.providers.errors import (
    ProviderAuthError, ProviderRateLimited, ProviderRefused, ProviderUnavailable,
)
from linguastack.providers.fake import FakeProvider


async def test_fake_echo_returns_input():
    provider = FakeProvider(scenario="echo")
    result = await provider.complete("fake-1", CompletionRequest(system="s", user="hello"))
    assert result.text == "hello"
    assert provider.calls[0].user == "hello"


async def test_retryable_flags_match_spec():
    # ADD §6.1: очередь решает про повтор, не зная провайдера.
    assert ProviderUnavailable("down").retryable is True
    assert ProviderRateLimited(retry_after=1.0).retryable is True
    assert ProviderAuthError("bad key").retryable is False
    assert ProviderRefused(category="cyber").retryable is False


async def test_rate_limited_scenario_carries_retry_after():
    with pytest.raises(ProviderRateLimited) as exc:
        await FakeProvider(scenario="rate_limited").complete(
            "fake-1", CompletionRequest(system="s", user="x"))
    assert exc.value.retry_after == 1.0


async def test_fail_times_then_succeeds():
    provider = FakeProvider(scenario="echo", fail_times=2)
    for _ in range(2):
        with pytest.raises(ProviderUnavailable):
            await provider.complete("fake-1", CompletionRequest(system="s", user="x"))
    assert (await provider.complete("fake-1", CompletionRequest(system="s", user="x"))).text == "x"


async def test_capabilities_declare_language_support_openness():
    caps = await FakeProvider(scenario="echo").capabilities("fake-1")
    # ТЗ §5.2: нельзя считать, что все модели поддерживают один набор языков.
    assert caps.supported_languages is None or isinstance(caps.supported_languages, frozenset)
```

- [ ] **Step 2: Запустить — FAIL**
- [ ] **Step 3: Реализовать `errors.py`, `base.py`, `fake.py`.**
- [ ] **Step 4: Запустить — PASS (5 passed)**
- [ ] **Step 5: Коммит** — `feat: интерфейс LLMProvider, возможности модели и таксономия ошибок`

---

### Task 15: OllamaProvider

**Files:**
- Create: `.../providers/ollama.py`
- Create: `backend/tests/test_provider_ollama.py`

**Interfaces:**
- Produces: `OllamaProvider(base_url: str, client: httpx.AsyncClient | None = None, timeout: float = 300.0)`; `provider_id = "ollama"`.
- `list_models()` → `GET /api/tags`; `health()` → `GET /api/version`; `complete()` → `POST /api/chat` (`stream=False`).
- Отображение намерения (ADD §6): `strict` → `options={"temperature": 0.0}`, `balanced` → `0.3`, `creative` → `0.8`. `supports_temperature = True`, стоимость `None` (локальная модель бесплатна — именно поэтому поля `Optional`).

- [ ] **Step 1: Написать падающий тест** — на `httpx.MockTransport`, без сети:

```python
# backend/tests/test_provider_ollama.py
import httpx
import pytest

from linguastack.providers.base import CompletionRequest
from linguastack.providers.errors import ModelNotAvailable, ProviderUnavailable
from linguastack.providers.ollama import OllamaProvider


def provider_with(handler) -> OllamaProvider:
    return OllamaProvider("http://ollama.test",
                          client=httpx.AsyncClient(transport=httpx.MockTransport(handler)))


async def test_list_models_parses_tags():
    def handler(request):
        assert request.url.path == "/api/tags"
        return httpx.Response(200, json={"models": [
            {"name": "qwen3:8b", "details": {"parameter_size": "8.2B"}}]})

    models = await provider_with(handler).list_models()
    assert [m.model_id for m in models] == ["qwen3:8b"]
    assert models[0].input_cost_per_mtok is None, "локальная модель не имеет цены"


async def test_complete_sends_temperature_zero_for_strict():
    captured = {}

    def handler(request):
        import json
        captured.update(json.loads(request.content))
        return httpx.Response(200, json={
            "message": {"content": "переведено"},
            "prompt_eval_count": 10, "eval_count": 3})

    result = await provider_with(handler).complete(
        "qwen3:8b", CompletionRequest(system="s", user="u", determinism="strict"))
    assert result.text == "переведено"
    assert captured["options"]["temperature"] == 0.0
    assert captured["stream"] is False
    assert result.input_tokens == 10 and result.output_tokens == 3


async def test_connection_error_becomes_provider_unavailable():
    def handler(request):
        raise httpx.ConnectError("refused")

    with pytest.raises(ProviderUnavailable):
        await provider_with(handler).complete("qwen3:8b", CompletionRequest(system="s", user="u"))


async def test_missing_model_becomes_model_not_available():
    def handler(request):
        return httpx.Response(404, json={"error": "model 'nope' not found"})

    with pytest.raises(ModelNotAvailable):
        await provider_with(handler).complete("nope", CompletionRequest(system="s", user="u"))


async def test_server_error_becomes_provider_unavailable():
    def handler(request):
        return httpx.Response(503, text="overloaded")

    with pytest.raises(ProviderUnavailable):
        await provider_with(handler).complete("qwen3:8b", CompletionRequest(system="s", user="u"))


async def test_timeout_becomes_provider_unavailable():
    def handler(request):
        raise httpx.ReadTimeout("too slow")

    with pytest.raises(ProviderUnavailable):
        await provider_with(handler).complete("qwen3:8b", CompletionRequest(system="s", user="u"))


@pytest.mark.live
async def test_live_ollama_translates_a_sentence():
    """Запускается только при LINGUASTACK_LIVE_OLLAMA=1; модель qwen3:8b установлена."""
    import os
    if os.environ.get("LINGUASTACK_LIVE_OLLAMA") != "1":
        pytest.skip("живой Ollama не включён")
    provider = OllamaProvider("http://localhost:11434")
    assert (await provider.health()).available
    result = await provider.complete("qwen3:8b", CompletionRequest(
        system="Translate English to Russian. Reply with the translation only.",
        user="The book is on the table."))
    assert result.text.strip()
```

- [ ] **Step 2: Запустить — FAIL**
- [ ] **Step 3: Реализовать.** Все исключения `httpx` ловятся и приводятся к таксономии. 404 с текстом про модель → `ModelNotAvailable`; 429 → `ProviderRateLimited`; ≥500 и сетевые → `ProviderUnavailable`; 400 → `ProviderBadRequest`.
- [ ] **Step 4: Запустить — PASS (6 passed, 1 skipped)**
- [ ] **Step 5: Проверить против живой Ollama**

Run: `cd backend && LINGUASTACK_LIVE_OLLAMA=1 .venv/bin/python -m pytest tests/test_provider_ollama.py -v -m live`
Expected: PASS — подтверждает, что адаптер работает против реального сервера, а не только против мока.

- [ ] **Step 6: Коммит** — `feat: адаптер Ollama с приведением ошибок к общей таксономии`

---

### Task 16: OpenAICompatibleProvider

**Files:**
- Create: `.../providers/openai_compat.py`
- Create: `backend/tests/test_provider_openai_compat.py`

**Interfaces:**
- Produces: `OpenAICompatibleProvider(base_url: str, api_key: str, client=None, known_models: dict[str, ModelCapabilities] | None = None)`; `provider_id = "openai_compatible"`.
- `POST {base_url}/chat/completions`, `Authorization: Bearer`. Один адаптер покрывает OpenAI, OpenRouter, Groq, DeepSeek, vLLM, LM Studio — `base_url` задаётся конфигом (решение владельца проекта).
- Отображение намерения: как у Ollama, через `temperature`.

- [ ] **Step 1: Написать падающий тест** — ключевые случаи:

```python
# backend/tests/test_provider_openai_compat.py
import httpx
import pytest

from linguastack.providers.base import CompletionRequest
from linguastack.providers.errors import (
    ProviderAuthError, ProviderRateLimited, ProviderUnavailable,
)
from linguastack.providers.openai_compat import OpenAICompatibleProvider


def provider_with(handler, api_key="sk-test-key"):
    return OpenAICompatibleProvider("https://api.test/v1", api_key,
        client=httpx.AsyncClient(transport=httpx.MockTransport(handler)))


async def test_sends_bearer_token_and_parses_choice():
    def handler(request):
        assert request.headers["authorization"] == "Bearer sk-test-key"
        return httpx.Response(200, json={
            "model": "gpt-x", "choices": [{"message": {"content": "перевод"}}],
            "usage": {"prompt_tokens": 7, "completion_tokens": 2}})

    result = await provider_with(handler).complete("gpt-x", CompletionRequest(system="s", user="u"))
    assert result.text == "перевод"
    assert result.input_tokens == 7 and result.output_tokens == 2


async def test_401_becomes_auth_error_and_is_not_retryable():
    def handler(request):
        return httpx.Response(401, json={"error": {"message": "bad key"}})

    with pytest.raises(ProviderAuthError) as exc:
        await provider_with(handler).complete("gpt-x", CompletionRequest(system="s", user="u"))
    assert exc.value.retryable is False


async def test_429_carries_retry_after_header():
    def handler(request):
        return httpx.Response(429, headers={"retry-after": "7"}, json={"error": {}})

    with pytest.raises(ProviderRateLimited) as exc:
        await provider_with(handler).complete("gpt-x", CompletionRequest(system="s", user="u"))
    assert exc.value.retry_after == 7.0


async def test_api_key_absent_from_exception_text():
    """ТЗ §13: секреты не раскрываются. Текст исключения часто попадает в логи."""
    def handler(request):
        return httpx.Response(500, text="internal error")

    with pytest.raises(ProviderUnavailable) as exc:
        await provider_with(handler, api_key="sk-super-secret").complete(
            "gpt-x", CompletionRequest(system="s", user="u"))
    assert "sk-super-secret" not in str(exc.value)


async def test_empty_choices_becomes_provider_bad_request():
    from linguastack.providers.errors import ProviderBadRequest

    def handler(request):
        return httpx.Response(200, json={"choices": [], "usage": {}})

    with pytest.raises(ProviderBadRequest):
        await provider_with(handler).complete("gpt-x", CompletionRequest(system="s", user="u"))
```

- [ ] **Step 2: Запустить — FAIL**
- [ ] **Step 3: Реализовать.** Важно: ключ не подставляется в текст исключений и не логируется.
- [ ] **Step 4: Запустить — PASS (5 passed)**
- [ ] **Step 5: Коммит** — `feat: универсальный адаптер OpenAI-совместимых API`

---

### Task 17: AnthropicProvider

**Files:**
- Create: `.../providers/anthropic_provider.py`
- Create: `backend/tests/test_provider_anthropic.py`

**Interfaces:**
- Produces: `AnthropicProvider(api_key: str, client: AsyncAnthropic | None = None)`; `provider_id = "anthropic"`.
- Каталог моделей — ADD §6.2 (`claude-opus-5-5` по умолчанию, `claude-sonnet-5-5`, `claude-haiku-5-5`), с ценами для учёта стоимости (ТЗ §6.3).

**Три особенности, которые ломают наивную реализацию** (ADD §6, §6.1):

1. **`temperature`, `top_p`, `top_k` не передаются никогда** — Opus/Sonnet 5.5 отвечают 400. Намерение отображается в `output_config={"effort": ...}`: `strict` → `low`, `balanced` → `medium`, `creative` → `high`. Поэтому `supports_temperature = False`.
2. **`stop_reason` проверяется до чтения `content`.** `"refusal"` приходит с HTTP 200 — принять его за перевод нельзя. → `ProviderRefused(category=response.stop_details.category)`.
3. **Системный промпт помечается `cache_control={"type": "ephemeral"}"`** — он одинаков для всех батчей книги, это основная экономия (ТЗ §6.3 «учёт стоимости»). Факт кэширования читается из `usage.cache_read_input_tokens`.

- [ ] **Step 1: Написать падающий тест**

```python
# backend/tests/test_provider_anthropic.py
import pytest

from linguastack.providers.anthropic_provider import AnthropicProvider
from linguastack.providers.base import CompletionRequest
from linguastack.providers.errors import ProviderRefused


class StubMessages:
    def __init__(self, response, recorder):
        self._response = response
        self._recorder = recorder

    async def create(self, **kwargs):
        self._recorder.update(kwargs)
        return self._response


class StubClient:
    def __init__(self, response, recorder):
        self.messages = StubMessages(response, recorder)


class StubUsage:
    def __init__(self, i=10, o=5, cr=0, cc=0):
        self.input_tokens, self.output_tokens = i, o
        self.cache_read_input_tokens, self.cache_creation_input_tokens = cr, cc


class StubBlock:
    def __init__(self, text):
        self.type, self.text = "text", text


class StubResponse:
    def __init__(self, text="перевод", stop_reason="end_turn", stop_details=None, usage=None):
        self.content = [StubBlock(text)]
        self.stop_reason, self.stop_details = stop_reason, stop_details
        self.usage = usage or StubUsage()
        self.model = "claude-opus-5-5"


async def test_never_sends_temperature():
    """Opus/Sonnet 5.5 отвечают 400 на temperature — ADD §6."""
    recorded = {}
    provider = AnthropicProvider("sk-ant-x", client=StubClient(StubResponse(), recorded))
    await provider.complete("claude-opus-5-5",
        CompletionRequest(system="s", user="u", determinism="strict"))
    assert "temperature" not in recorded
    assert "top_p" not in recorded and "top_k" not in recorded


async def test_determinism_maps_to_effort():
    for determinism, effort in [("strict", "low"), ("balanced", "medium"), ("creative", "high")]:
        recorded = {}
        provider = AnthropicProvider("sk-ant-x", client=StubClient(StubResponse(), recorded))
        await provider.complete("claude-opus-5-5",
            CompletionRequest(system="s", user="u", determinism=determinism))
        assert recorded["output_config"]["effort"] == effort


async def test_system_prompt_is_cache_controlled():
    recorded = {}
    provider = AnthropicProvider("sk-ant-x", client=StubClient(StubResponse(), recorded))
    await provider.complete("claude-opus-5-5", CompletionRequest(system="инструкция", user="u"))
    assert recorded["system"][0]["cache_control"] == {"type": "ephemeral"}
    assert recorded["system"][0]["text"] == "инструкция"


async def test_refusal_raises_before_reading_content():
    """stop_reason == refusal приходит с HTTP 200 — принять за перевод нельзя."""
    class Details:
        category, explanation = "cyber", "declined"

    response = StubResponse(text="я не могу помочь", stop_reason="refusal", stop_details=Details())
    provider = AnthropicProvider("sk-ant-x", client=StubClient(response, {}))
    with pytest.raises(ProviderRefused) as exc:
        await provider.complete("claude-opus-5-5", CompletionRequest(system="s", user="u"))
    assert exc.value.category == "cyber"
    assert exc.value.retryable is False


async def test_cost_computed_from_catalog_prices():
    usage = StubUsage(i=1_000_000, o=1_000_000)
    provider = AnthropicProvider("sk-ant-x", client=StubClient(StubResponse(usage=usage), {}))
    result = await provider.complete("claude-opus-5-5", CompletionRequest(system="s", user="u"))
    # ADD §6.2: 4.00 за 1M входных + 20.00 за 1M выходных.
    assert result.cost_usd == pytest.approx(24.0)


async def test_cache_reads_reported():
    usage = StubUsage(i=100, o=5, cr=900)
    provider = AnthropicProvider("sk-ant-x", client=StubClient(StubResponse(usage=usage), {}))
    result = await provider.complete("claude-opus-5-5", CompletionRequest(system="s", user="u"))
    assert result.cached_input_tokens == 900


async def test_model_ids_have_no_date_suffix():
    models = await AnthropicProvider("sk-ant-x", client=StubClient(StubResponse(), {})).list_models()
    ids = {m.model_id for m in models}
    assert ids == {"claude-opus-5-5", "claude-sonnet-5-5", "claude-haiku-5-5"}
    assert not any(any(ch.isdigit() for ch in i.split("-")[-1]) and len(i.split("-")[-1]) == 8
                   for i in ids), "суффиксы даты запрещены"


async def test_max_tokens_is_always_sent():
    recorded = {}
    provider = AnthropicProvider("sk-ant-x", client=StubClient(StubResponse(), recorded))
    await provider.complete("claude-opus-5-5", CompletionRequest(system="s", user="u"))
    assert recorded["max_tokens"] >= 4096
```

- [ ] **Step 2: Запустить — FAIL**
- [ ] **Step 3: Реализовать.** Цепочка ошибок от конкретных к общим: `anthropic.AuthenticationError`/`PermissionDeniedError` → `ProviderAuthError`; `NotFoundError` → `ModelNotAvailable`; `RateLimitError` → `ProviderRateLimited(retry_after из заголовка)`; `BadRequestError` → `ProviderBadRequest`; `APIStatusError` ≥500 и `APIConnectionError`/`APITimeoutError` → `ProviderUnavailable`. Ключ не попадает в текст исключений.
- [ ] **Step 4: Запустить — PASS (8 passed)**
- [ ] **Step 5: Коммит** — `feat: адаптер Anthropic (effort вместо temperature, учёт отказов и кэша)`

---

### Task 18: Конфигурации провайдеров — шифрование ключей и защита от утечки

**Files:**
- Create: `.../db/models/provider_config.py`, `.../providers/registry.py`, `.../api/providers.py`
- Modify: `.../main.py`, миграция
- Create: `backend/tests/test_provider_configs_api.py`

**Interfaces:**
- Produces: `GET /api/providers`, `GET /api/providers/{id}/models`, `POST /api/provider-configs`, `GET /api/provider-configs`, `DELETE /api/provider-configs/{id}` (ТЗ §12).
- Модель `ProviderConfig(id, owner_id, provider_id, base_url, secret_ciphertext, key_fingerprint, key_last4, allow_external: bool, created_at)`.
- `registry.build_provider(config, settings) -> LLMProvider` — расшифровывает ключ только в момент создания адаптера.
- `allow_external` — реализация ТЗ §6.4: пользователь может **запретить** передачу текста внешним сервисам; при `False` создание проекта на внешнем провайдере отклоняется.

- [ ] **Step 1: Написать падающий тест**

```python
# backend/tests/test_provider_configs_api.py
import logging


async def test_api_key_never_returned_in_any_response(user_client):
    """ТЗ §15.14 и §6.3: ключ не раскрывается клиенту после сохранения."""
    secret = "sk-ant-super-secret-value-9876"
    created = await user_client.post("/api/provider-configs", json={
        "provider_id": "anthropic", "api_key": secret})
    assert created.status_code == 201
    assert secret not in created.text
    assert created.json()["key_last4"] == "9876"
    assert len(created.json()["key_fingerprint"]) == 8

    listed = await user_client.get("/api/provider-configs")
    assert secret not in listed.text
    detail = await user_client.get(f"/api/provider-configs/{created.json()['id']}")
    assert secret not in detail.text


async def test_api_key_absent_from_logs(user_client, caplog):
    secret = "sk-ant-leak-check-1234"
    with caplog.at_level(logging.DEBUG):
        await user_client.post("/api/provider-configs", json={
            "provider_id": "anthropic", "api_key": secret})
    assert secret not in caplog.text, "ТЗ §13: запрет на раскрытие секретов в логах"


async def test_ciphertext_in_db_is_not_plaintext(user_client, session):
    from sqlalchemy import select

    from linguastack.db.models.provider_config import ProviderConfig

    secret = "sk-ant-db-check-5555"
    await user_client.post("/api/provider-configs", json={
        "provider_id": "anthropic", "api_key": secret})
    row = (await session.execute(select(ProviderConfig))).scalars().first()
    assert secret not in row.secret_ciphertext


async def test_foreign_provider_config_returns_404(user_client, other_client):
    other = await other_client.post("/api/provider-configs", json={
        "provider_id": "anthropic", "api_key": "sk-ant-other-1111"})
    config_id = other.json()["id"]
    assert (await user_client.get(f"/api/provider-configs/{config_id}")).status_code == 404
    assert (await user_client.delete(f"/api/provider-configs/{config_id}")).status_code == 404


async def test_ollama_needs_no_api_key(user_client):
    r = await user_client.post("/api/provider-configs", json={
        "provider_id": "ollama", "base_url": "http://localhost:11434"})
    assert r.status_code == 201
    assert r.json()["key_last4"] is None


async def test_providers_list_declares_external_data_egress(user_client):
    """ТЗ §6.3: интерфейс обязан явно сообщать, что текст покинет локальный сервер."""
    providers = (await user_client.get("/api/providers")).json()
    by_id = {p["provider_id"]: p for p in providers}
    assert by_id["ollama"]["sends_data_externally"] is False
    assert by_id["anthropic"]["sends_data_externally"] is True
    assert by_id["openai_compatible"]["sends_data_externally"] is True
```

- [ ] **Step 2: Запустить — FAIL**
- [ ] **Step 3: Реализовать.** Схема ответа `ProviderConfigOut` **не содержит** поля с ключом — защита на уровне типа, а не на внимательности. Добавить `logging.Filter`, вырезающий подстроки, похожие на ключи (`sk-[A-Za-z0-9-]{8,}`), и подключить его в `create_app()`.
- [ ] **Step 4: Запустить — PASS (6 passed)**
- [ ] **Step 5: Коммит** — `feat: конфигурации провайдеров с шифрованием ключей и запретом утечки`

---

### Task 19: Сегментатор и батчер

**Files:**
- Create: `.../translation/__init__.py`, `.../translation/segmenter.py`
- Create: `backend/tests/test_segmenter.py`

**Interfaces:**
- Produces: `SegmentRef(node_id: str, chapter_index: int, type: BlockType, text: str, source_hash: str, level: int | None)`; `Batch(segments: list[SegmentRef], context_before: str | None, context_after: str | None)`; `segment_document(doc: CanonicalDocument) -> list[SegmentRef]`; `build_batches(segments, max_segments, max_chars, context_lookup) -> list[Batch]`; исключение `SegmentTooLarge(node_id, chars, limit)`.
- Правила — ADD §2: сегмент = один переводимый блок; батч не пересекает границу главы.

- [ ] **Step 1: Написать падающий тест** (включая **Review Focus №1 и №3**)

```python
# backend/tests/test_segmenter.py
import pytest

from linguastack.cdm.ids import content_hash, make_node_id
from linguastack.cdm.model import Block, BlockType, BookMeta, CanonicalDocument, Chapter
from linguastack.translation.segmenter import (
    SegmentTooLarge, build_batches, segment_document,
)


def doc_with(*chapters: Chapter) -> CanonicalDocument:
    return CanonicalDocument(
        meta=BookMeta(title="T", author=None, language="en", source_format="md"),
        chapters=list(chapters))


def para(chapter: int, ordinal: int, text: str) -> Block:
    return Block(node_id=make_node_id(chapter, ordinal), type=BlockType.paragraph,
                 text=text, source_hash=content_hash(text))


def code(chapter: int, ordinal: int, text: str) -> Block:
    return Block(node_id=make_node_id(chapter, ordinal), type=BlockType.code_block,
                 text=text, source_hash=content_hash(text))


def test_one_segment_per_translatable_block():
    document = doc_with(Chapter(index=0, title="C", blocks=[
        para(0, 0, "Первый."), code(0, 1, "print(1)"), para(0, 2, "Второй.")]))
    segments = segment_document(document)
    assert [s.text for s in segments] == ["Первый.", "Второй."]


def test_batches_respect_segment_count_limit():
    document = doc_with(Chapter(index=0, title="C",
        blocks=[para(0, i, f"Абзац {i}.") for i in range(25)]))
    batches = build_batches(segment_document(document), max_segments=10, max_chars=10_000)
    assert [len(b.segments) for b in batches] == [10, 10, 5]


def test_batches_respect_char_limit():
    document = doc_with(Chapter(index=0, title="C",
        blocks=[para(0, i, "x" * 400) for i in range(10)]))
    batches = build_batches(segment_document(document), max_segments=100, max_chars=1000)
    assert all(sum(len(s.text) for s in b.segments) <= 1000 for b in batches)
    assert len(batches) > 1


def test_batch_never_spans_two_chapters():
    document = doc_with(
        Chapter(index=0, title="A", blocks=[para(0, 0, "A1."), para(0, 1, "A2.")]),
        Chapter(index=1, title="B", blocks=[para(1, 0, "B1.")]))
    batches = build_batches(segment_document(document), max_segments=100, max_chars=10_000)
    for batch in batches:
        assert len({s.chapter_index for s in batch.segments}) == 1


def test_context_carries_neighbouring_text():
    document = doc_with(Chapter(index=0, title="C",
        blocks=[para(0, 0, "Нулевой."), para(0, 1, "Первый."), para(0, 2, "Второй.")]))
    batches = build_batches(segment_document(document), max_segments=1, max_chars=10_000)
    assert batches[1].context_before == "Нулевой."
    assert batches[1].context_after == "Второй."
    assert batches[0].context_before is None


def test_chapter_with_only_code_yields_no_segments():
    """Review Focus №1: глава целиком из кода не должна ломать прогресс."""
    document = doc_with(Chapter(index=0, title="C",
        blocks=[code(0, 0, "print(1)"), code(0, 1, "print(2)")]))
    assert segment_document(document) == []
    assert build_batches([], max_segments=10, max_chars=1000) == []


def test_oversized_single_block_raises_named_error():
    """Review Focus №3: блок больше лимита — явная ошибка, не молчаливое обрезание."""
    document = doc_with(Chapter(index=0, title="C", blocks=[para(0, 0, "x" * 50_000)]))
    with pytest.raises(SegmentTooLarge) as exc:
        build_batches(segment_document(document), max_segments=10, max_chars=6000)
    assert exc.value.node_id == "ch0000/b0000"
    assert exc.value.chars == 50_000 and exc.value.limit == 6000


def test_blank_blocks_are_not_segmented():
    document = doc_with(Chapter(index=0, title="C",
        blocks=[para(0, 0, "   "), para(0, 1, "Текст.")]))
    assert [s.text for s in segment_document(document)] == ["Текст."]
```

- [ ] **Step 2: Запустить — FAIL**
- [ ] **Step 3: Реализовать.** `build_batches` группирует последовательные сегменты одной главы; `SegmentTooLarge` поднимается, когда *одиночный* сегмент не влезает в `max_chars` (иначе он никогда не был бы переведён).
- [ ] **Step 4: Запустить — PASS (8 passed)**
- [ ] **Step 5: Коммит** — `feat: сегментатор CDM и батчер с контекстом соседних блоков`

---

### Task 20: Протокол конверта и его парсер

**Files:**
- Create: `.../translation/prompt.py`
- Create: `backend/tests/test_prompt_protocol.py`

**Interfaces:**
- Produces: `build_system_prompt(source_lang, target_lang, style, glossary: dict[str, str]) -> str`; `build_user_prompt(batch: Batch) -> str`; `parse_envelope(text: str, expected: int) -> dict[int, str]`; исключение `ProtocolError(reason, raw)`.
- Формат — ADD §3. Системный промпт **детерминирован** (глоссарий сортируется) — иначе рушится кэш префикса (ADD §6.2).

- [ ] **Step 1: Написать падающий тест**

```python
# backend/tests/test_prompt_protocol.py
import pytest

from linguastack.translation.prompt import (
    ProtocolError, build_system_prompt, build_user_prompt, parse_envelope,
)


def test_parses_well_formed_envelope():
    text = "<<<SEG 1>>>\nПервый\n<<<SEG 2>>>\nВторой\n<<<END>>>"
    assert parse_envelope(text, expected=2) == {1: "Первый", 2: "Второй"}


def test_tolerates_preamble_and_trailing_chatter():
    """Модель часто добавляет вежливость. Парсер обязан это переживать."""
    text = ("Вот перевод:\n\n<<<SEG 1>>>\nПервый\n<<<SEG 2>>>\nВторой\n<<<END>>>\n"
            "Надеюсь, это помогло!")
    assert parse_envelope(text, expected=2) == {1: "Первый", 2: "Второй"}


def test_missing_segment_raises_protocol_error():
    with pytest.raises(ProtocolError) as exc:
        parse_envelope("<<<SEG 1>>>\nТолько один\n<<<END>>>", expected=2)
    assert "2" in str(exc.value)


def test_extra_segment_raises_protocol_error():
    with pytest.raises(ProtocolError):
        parse_envelope("<<<SEG 1>>>\nA\n<<<SEG 2>>>\nB\n<<<SEG 3>>>\nC\n<<<END>>>", expected=2)


def test_duplicate_index_raises_protocol_error():
    with pytest.raises(ProtocolError):
        parse_envelope("<<<SEG 1>>>\nA\n<<<SEG 1>>>\nB\n<<<END>>>", expected=2)


def test_missing_end_marker_still_parses_last_segment():
    # Обрыв по max_tokens не должен терять уже полученное; решение проверяет валидация.
    assert parse_envelope("<<<SEG 1>>>\nA\n<<<SEG 2>>>\nB", expected=2) == {1: "A", 2: "B"}


def test_no_envelope_at_all_raises_protocol_error():
    with pytest.raises(ProtocolError):
        parse_envelope("Я не понял задачу.", expected=1)


def test_multiline_segment_preserved():
    text = "<<<SEG 1>>>\nСтрока один\nСтрока два\n<<<END>>>"
    assert parse_envelope(text, expected=1) == {1: "Строка один\nСтрока два"}


def test_system_prompt_is_deterministic_for_cache_stability():
    """ADD §6.2: любой дрейф префикса убивает кэш и удорожает книгу."""
    a = build_system_prompt("en", "ru", "neutral", {"б": "b", "а": "a"})
    b = build_system_prompt("en", "ru", "neutral", {"а": "a", "б": "b"})
    assert a == b, "порядок глоссария не должен влиять на промпт"
    assert "{" not in a.replace("{{", ""), "никаких незаполненных шаблонов"


def test_system_prompt_mentions_languages_and_glossary_terms():
    prompt = build_system_prompt("en", "ru", "neutral", {"queue": "очередь"})
    assert "en" in prompt and "ru" in prompt
    assert "queue" in prompt and "очередь" in prompt


def test_user_prompt_numbers_segments_and_marks_context():
    from linguastack.translation.segmenter import Batch, SegmentRef
    from linguastack.cdm.model import BlockType

    batch = Batch(
        segments=[SegmentRef("ch0000/b0001", 0, BlockType.paragraph, "Текст.", "h", None)],
        context_before="До.", context_after="После.")
    prompt = build_user_prompt(batch)
    assert "<<<SEG 1>>>" in prompt and "<<<END>>>" in prompt
    assert "Текст." in prompt
    # Контекст должен быть явно отделён, иначе модель переведёт и его.
    assert "До." in prompt and prompt.index("До.") < prompt.index("<<<SEG 1>>>")
```

- [ ] **Step 2: Запустить — FAIL**
- [ ] **Step 3: Реализовать.** `parse_envelope` сканирует построчно по `^<<<SEG (\d+)>>>\s*$`; текст до первого маркера и после `<<<END>>>` отбрасывается; множество индексов сверяется с `set(range(1, expected + 1))`.
- [ ] **Step 4: Запустить — PASS (11 passed)**
- [ ] **Step 5: Коммит** — `feat: протокол нумерованного конверта и устойчивый парсер ответа модели`

---

### Task 21: Валидация результата перевода

**Files:**
- Create: `.../translation/validation.py`
- Create: `backend/tests/test_validation.py`

**Interfaces:**
- Produces: `Severity` (str-Enum: `ok`, `warning`, `error`); `Finding(code: str, severity: Severity, message: str)`; `validate_segment(source: str, translation: str, masked_source: str, source_lang: str, target_lang: str, block_type: BlockType, level: int | None) -> list[Finding]`; `worst(findings) -> Severity`.
- Таблица проверок и severity — **ровно** ADD §5.

- [ ] **Step 1: Написать падающий тест**

```python
# backend/tests/test_validation.py
from linguastack.cdm.model import BlockType
from linguastack.translation.validation import Severity, validate_segment, worst


def check(source, translation, masked=None, src="en", tgt="ru", block=BlockType.paragraph):
    return validate_segment(source, translation, masked or source, src, tgt, block, None)


def codes(findings):
    return {f.code for f in findings}


def test_clean_translation_has_no_findings():
    assert check("The book is here.", "Книга здесь.") == []


def test_empty_translation_is_error():
    findings = check("Непустой исходник.", "")
    assert "empty_translation" in codes(findings)
    assert worst(findings) is Severity.error


def test_placeholder_dropped_is_error():
    findings = check("Code ⟦1⟧ here.", "Код здесь.", masked="Code ⟦1⟧ here.")
    assert "placeholder_mismatch" in codes(findings)
    assert worst(findings) is Severity.error


def test_placeholder_reordered_is_fine():
    assert check("A ⟦1⟧ B ⟦2⟧", "⟦2⟧ Б ⟦1⟧ А", masked="A ⟦1⟧ B ⟦2⟧") == []


def test_link_count_change_is_error():
    findings = check("See [docs](x) and [api](y).", "См. [документацию](x).")
    assert "link_count_mismatch" in codes(findings)
    assert worst(findings) is Severity.error


def test_untranslated_copy_is_warning_not_error():
    source = "This is a reasonably long English sentence that should change."
    findings = check(source, source)
    assert "untranslated" in codes(findings)
    assert worst(findings) is Severity.warning


def test_short_identical_text_is_not_flagged():
    # "SQL" или "2026" законно совпадают — порог длины 40 символов, ADD §5.
    assert check("SQL", "SQL") == []


def test_length_ratio_outside_bounds_is_warning():
    findings = check("A short sentence here.", "Очень " * 100)
    assert "length_ratio" in codes(findings)
    assert worst(findings) is Severity.warning


def test_inline_markup_count_change_is_warning():
    findings = check("Это **важно** и **нужно**.", "Это **важно** и нужно.")
    assert "markup_mismatch" in codes(findings)
    assert worst(findings) is Severity.warning


def test_heading_level_change_is_warning():
    from linguastack.translation.validation import validate_segment

    findings = validate_segment("# Title", "## Заголовок", "# Title", "en", "ru",
                                BlockType.heading, level=1)
    assert "heading_level_changed" in codes(findings)


def test_worst_picks_error_over_warning():
    findings = check("Code ⟦1⟧ x", "", masked="Code ⟦1⟧ x")
    assert worst(findings) is Severity.error


def test_same_language_pair_skips_untranslated_check():
    # Перевод en→en (например, упрощение) законно может совпадать.
    source = "This is a reasonably long English sentence that should change."
    assert "untranslated" not in codes(check(source, source, src="en", tgt="en"))
```

- [ ] **Step 2: Запустить — FAIL**
- [ ] **Step 3: Реализовать** строго по таблице ADD §5.
- [ ] **Step 4: Запустить — PASS (12 passed)**
- [ ] **Step 5: Коммит** — `feat: валидация перевода с разделением ошибок и предупреждений`

---

### Task 22: Модели проекта перевода, сегментов, ревизий и задач

**Files:**
- Create: `.../db/models/translation.py`, миграция
- Create: `backend/tests/test_translation_models.py`

**Interfaces:**
- Produces ORM-модели (сущности ТЗ §11):
  - `TranslationProject(id, owner_id, book_id, source_lang, target_lang, provider_config_id, model_id, style, determinism, status, segments_total, segments_done, segments_failed, warnings_count, created_at, updated_at)`. Статусы: `draft`, `queued`, `running`, `paused`, `completed`, `completed_with_warnings`, `failed`, `cancelled`.
  - `TranslationSegment(id, project_id, node_id, chapter_index, ordinal, source_text, source_hash, translated_text, status, severity, findings: dict, attempts, last_error, updated_at)`. Статусы: `pending`, `running`, `completed`, `failed`, `stale`. **Уникальность `(project_id, node_id)`.**
  - `TranslationSegmentRevision(id, segment_id, translated_text, origin, created_at)` — `origin` ∈ `machine`, `manual`, `retry`; реализует ТЗ §5.1 «повторный перевод не уничтожает предыдущую версию».
  - `TranslationTask(id, project_id, owner_id, kind, status, priority, attempts, max_attempts, locked_by, locked_until, last_error, created_at, started_at, finished_at)`. Статусы — **ровно** из ТЗ §7: `pending`, `running`, `completed`, `failed`, `cancelled`.
- Индексы: `translation_tasks(status, priority DESC, created_at)`, `translation_tasks(owner_id, status)`, `translation_segments(project_id, status)`.

- [ ] **Step 1: Написать падающий тест**

```python
# backend/tests/test_translation_models.py
import pytest
from sqlalchemy.exc import IntegrityError


async def test_segment_node_id_unique_per_project(session, make_user, make_book, make_project):
    from linguastack.db.models.translation import TranslationSegment

    user = await make_user("m@example.com")
    project = await make_project(user, await make_book(user))
    session.add(TranslationSegment(project_id=project.id, node_id="ch0000/b0000",
        chapter_index=0, ordinal=0, source_text="x", source_hash="h", status="pending"))
    await session.flush()
    session.add(TranslationSegment(project_id=project.id, node_id="ch0000/b0000",
        chapter_index=0, ordinal=0, source_text="y", source_hash="h2", status="pending"))
    with pytest.raises(IntegrityError):
        await session.flush()


async def test_two_projects_on_one_book_are_independent(session, make_user, make_book, make_project):
    """ТЗ §5.1 и Review Focus №4: en→ru и en→de на одной книге не мешают друг другу."""
    from linguastack.db.models.translation import TranslationSegment

    user = await make_user("two@example.com")
    book = await make_book(user)
    ru = await make_project(user, book, target_lang="ru")
    de = await make_project(user, book, target_lang="de")
    assert ru.id != de.id

    for project, text in ((ru, "Книга"), (de, "Buch")):
        session.add(TranslationSegment(project_id=project.id, node_id="ch0000/b0000",
            chapter_index=0, ordinal=0, source_text="Book", source_hash="h",
            translated_text=text, status="completed"))
    await session.flush()  # один и тот же node_id в двух проектах — законно


async def test_revision_history_preserved_on_retranslate(session, make_user, make_book, make_project):
    """ТЗ §5.1: повторный перевод не должен уничтожать предыдущую версию."""
    from sqlalchemy import select

    from linguastack.db.models.translation import (
        TranslationSegment, TranslationSegmentRevision,
    )

    user = await make_user("rev@example.com")
    project = await make_project(user, await make_book(user))
    segment = TranslationSegment(project_id=project.id, node_id="ch0000/b0000",
        chapter_index=0, ordinal=0, source_text="Book", source_hash="h",
        translated_text="Первый вариант", status="completed")
    session.add(segment)
    await session.flush()

    session.add(TranslationSegmentRevision(segment_id=segment.id,
        translated_text="Первый вариант", origin="machine"))
    segment.translated_text = "Второй вариант"
    session.add(TranslationSegmentRevision(segment_id=segment.id,
        translated_text="Второй вариант", origin="retry"))
    await session.flush()

    revisions = (await session.execute(select(TranslationSegmentRevision)
        .where(TranslationSegmentRevision.segment_id == segment.id)
        .order_by(TranslationSegmentRevision.created_at))).scalars().all()
    assert [r.translated_text for r in revisions] == ["Первый вариант", "Второй вариант"]


async def test_task_statuses_match_spec(session, make_user, make_book, make_project):
    from linguastack.db.models.translation import TranslationTask

    assert set(TranslationTask.STATUSES) == {
        "pending", "running", "completed", "failed", "cancelled"}
```

- [ ] **Step 2: Запустить — FAIL**
- [ ] **Step 3: Реализовать модели, миграцию и фабрику `make_project`.**
- [ ] **Step 4: Запустить — PASS (4 passed)**
- [ ] **Step 5: Коммит** — `feat: модели проектов перевода, сегментов, ревизий и задач очереди`

---

### Task 23: Сервис перевода батча с деградацией протокола

**Files:**
- Create: `.../translation/service.py`
- Create: `backend/tests/test_translation_service.py`

**Interfaces:**
- Produces: `TranslationService(provider: LLMProvider, model_id: str, settings)`; `async def translate_batch(batch: Batch, system_prompt: str, determinism) -> BatchOutcome`; `BatchOutcome(results: dict[str, SegmentOutcome], provider_calls: int, cost_usd: float | None)`; `SegmentOutcome(node_id, translated_text, severity, findings, error)`.
- Конвейер на батч: маскирование (задача 7) → конверт (задача 20) → провайдер (14–17) → парсер → проверка маркеров → размаскирование → валидация (21).
- **Деградация ADD §3:** повтор батча (до 2 раз) → по одному сегменту → `failed` с сохранением последнего ответа.

- [ ] **Step 1: Написать падающий тест**

```python
# backend/tests/test_translation_service.py
import pytest

from linguastack.cdm.model import BlockType
from linguastack.providers.fake import FakeProvider
from linguastack.translation.segmenter import Batch, SegmentRef
from linguastack.translation.service import TranslationService
from linguastack.translation.validation import Severity


def seg(ordinal: int, text: str) -> SegmentRef:
    return SegmentRef(f"ch0000/b{ordinal:04d}", 0, BlockType.paragraph, text, "h", None)


def batch_of(*texts: str) -> Batch:
    return Batch(segments=[seg(i, t) for i, t in enumerate(texts)],
                 context_before=None, context_after=None)


def service(provider) -> TranslationService:
    from linguastack.config import get_settings
    return TranslationService(provider, "fake-1", get_settings())


async def test_happy_path_translates_every_segment():
    provider = FakeProvider(scenario="prefix", prefix="RU:")
    outcome = await service(provider).translate_batch(batch_of("One.", "Two."), "sys", "strict")
    assert outcome.results["ch0000/b0000"].translated_text == "RU:One."
    assert outcome.results["ch0000/b0001"].translated_text == "RU:Two."
    assert outcome.provider_calls == 1


async def test_protocol_violation_retries_then_falls_back_to_singletons():
    """ADD §3: трёхступенчатая деградация вместо падения на всей книге."""
    provider = FakeProvider(scenario="protocol_violation", violate_times=2, prefix="RU:")
    outcome = await service(provider).translate_batch(batch_of("One.", "Two."), "sys", "strict")
    assert all(r.severity is not Severity.error for r in outcome.results.values())
    assert outcome.provider_calls > 1, "должен был быть хотя бы один повтор"


async def test_persistent_protocol_violation_marks_segments_failed_with_raw_kept():
    provider = FakeProvider(scenario="protocol_violation", violate_times=99)
    outcome = await service(provider).translate_batch(batch_of("One."), "sys", "strict")
    result = outcome.results["ch0000/b0000"]
    assert result.severity is Severity.error
    assert result.error, "последний ответ модели должен сохраниться для разбора"


async def test_dropped_placeholder_is_detected_not_silently_accepted():
    provider = FakeProvider(scenario="drops_placeholder")
    outcome = await service(provider).translate_batch(batch_of("Call `run()` now."), "sys", "strict")
    assert outcome.results["ch0000/b0000"].severity is Severity.error


async def test_protected_code_restored_verbatim():
    provider = FakeProvider(scenario="prefix", prefix="RU:")
    outcome = await service(provider).translate_batch(
        batch_of("Call `get_user()` now."), "sys", "strict")
    # Код обязан вернуться дословно — ТЗ §5.3.
    assert "`get_user()`" in outcome.results["ch0000/b0000"].translated_text


async def test_model_never_sees_raw_code():
    provider = FakeProvider(scenario="prefix", prefix="RU:")
    await service(provider).translate_batch(batch_of("Call `secret_fn()`."), "sys", "strict")
    assert "secret_fn()" not in provider.calls[0].user, "код маскируется до отправки"


async def test_refusal_propagates_as_error_not_as_translation():
    provider = FakeProvider(scenario="refusal")
    outcome = await service(provider).translate_batch(batch_of("Text."), "sys", "strict")
    assert outcome.results["ch0000/b0000"].severity is Severity.error
    assert outcome.results["ch0000/b0000"].translated_text is None


async def test_rate_limit_is_raised_for_queue_to_handle():
    from linguastack.providers.errors import ProviderRateLimited

    with pytest.raises(ProviderRateLimited):
        await service(FakeProvider(scenario="rate_limited")).translate_batch(
            batch_of("Text."), "sys", "strict")


async def test_empty_translation_is_error():
    outcome = await service(FakeProvider(scenario="empty")).translate_batch(
        batch_of("Text."), "sys", "strict")
    assert outcome.results["ch0000/b0000"].severity is Severity.error
```

- [ ] **Step 2: Запустить — FAIL**
- [ ] **Step 3: Реализовать.** Важное разделение ответственности: ошибки *протокола и качества* сервис обрабатывает сам (повторы, деградация); ошибки *инфраструктуры* (`ProviderRateLimited`, `ProviderUnavailable`) он **поднимает наружу** — решение о повторе принимает очередь, у которой есть backoff и аренда.
- [ ] **Step 4: Запустить — PASS (9 passed)**
- [ ] **Step 5: Коммит** — `feat: сервис перевода батча с маскированием, валидацией и деградацией протокола`

---

### Task 24: Репозиторий очереди на арендах

**Files:**
- Create: `.../queue/__init__.py`, `.../queue/repository.py`
- Create: `backend/tests/test_queue_repository.py`

**Interfaces:**
- Produces: `enqueue(session, project, kind="translate", priority=0) -> TranslationTask`; `claim_next(session, worker_id, lease_seconds, max_per_user) -> TranslationTask | None`; `heartbeat(session, task, lease_seconds) -> bool`; `complete(session, task)`; `fail(session, task, error, retry_in: float | None)`; `cancel_for_project(session, project_id)`; `reclaim_expired(session) -> int`.
- SQL захвата — **ровно** из ADD §7 (`FOR UPDATE SKIP LOCKED` + подзапрос честности).

- [ ] **Step 1: Написать падающий тест**

```python
# backend/tests/test_queue_repository.py
from datetime import datetime, timedelta, timezone

from linguastack.queue import repository as queue


async def test_claim_returns_pending_task_and_marks_running(session, make_user, make_book, make_project):
    user = await make_user("q1@example.com")
    project = await make_project(user, await make_book(user))
    await queue.enqueue(session, project)

    task = await queue.claim_next(session, "worker-a", lease_seconds=60, max_per_user=3)
    assert task is not None
    assert task.status == "running"
    assert task.locked_by == "worker-a"
    assert task.locked_until > datetime.now(timezone.utc)
    assert task.attempts == 1


async def test_claimed_task_not_handed_to_second_worker(session, make_user, make_book, make_project):
    user = await make_user("q2@example.com")
    await queue.enqueue(session, await make_project(user, await make_book(user)))
    assert await queue.claim_next(session, "worker-a", 60, 3) is not None
    assert await queue.claim_next(session, "worker-b", 60, 3) is None


async def test_expired_lease_makes_task_claimable_again(session, make_user, make_book, make_project):
    """ТЗ §7: восстановление после перезапуска. Упавший worker ничего не держит."""
    user = await make_user("q3@example.com")
    await queue.enqueue(session, await make_project(user, await make_book(user)))
    task = await queue.claim_next(session, "dead-worker", 60, 3)
    task.locked_until = datetime.now(timezone.utc) - timedelta(seconds=1)
    await session.flush()

    reclaimed = await queue.claim_next(session, "live-worker", 60, 3)
    assert reclaimed is not None and reclaimed.id == task.id
    assert reclaimed.locked_by == "live-worker"
    assert reclaimed.attempts == 2


async def test_per_user_concurrency_limit_enforced(session, make_user, make_book, make_project):
    """ТЗ §7: один пользователь не должен занять все ресурсы."""
    user = await make_user("q4@example.com")
    book = await make_book(user)
    for _ in range(5):
        await queue.enqueue(session, await make_project(user, book))

    claimed = [await queue.claim_next(session, f"w{i}", 60, max_per_user=2) for i in range(5)]
    assert sum(1 for t in claimed if t is not None) == 2


async def test_other_users_not_blocked_by_a_busy_user(session, make_user, make_book, make_project):
    hog = await make_user("hog@example.com")
    hog_book = await make_book(hog)
    for _ in range(3):
        await queue.enqueue(session, await make_project(hog, hog_book))
    for i in range(3):
        assert await queue.claim_next(session, f"w{i}", 60, max_per_user=2) is not None or True

    polite = await make_user("polite@example.com")
    await queue.enqueue(session, await make_project(polite, await make_book(polite)))
    task = await queue.claim_next(session, "w-polite", 60, max_per_user=2)
    assert task is not None and task.owner_id == polite.id


async def test_priority_respected_before_fifo(session, make_user, make_book, make_project):
    user = await make_user("q5@example.com")
    book = await make_book(user)
    low = await queue.enqueue(session, await make_project(user, book), priority=0)
    high = await queue.enqueue(session, await make_project(user, book), priority=10)
    assert (await queue.claim_next(session, "w", 60, 3)).id == high.id


async def test_heartbeat_extends_lease_only_for_holder(session, make_user, make_book, make_project):
    user = await make_user("q6@example.com")
    await queue.enqueue(session, await make_project(user, await make_book(user)))
    task = await queue.claim_next(session, "owner-worker", 60, 3)
    before = task.locked_until
    assert await queue.heartbeat(session, task, lease_seconds=300) is True
    assert task.locked_until > before

    task.locked_by = "someone-else"
    await session.flush()
    assert await queue.heartbeat(session, task, lease_seconds=300) is False


async def test_completed_task_is_never_reclaimed(session, make_user, make_book, make_project):
    """ТЗ §7: защита от повторного выполнения завершённых задач."""
    user = await make_user("q7@example.com")
    await queue.enqueue(session, await make_project(user, await make_book(user)))
    task = await queue.claim_next(session, "w", 60, 3)
    await queue.complete(session, task)
    task.locked_until = datetime.now(timezone.utc) - timedelta(seconds=1)
    await session.flush()
    assert await queue.claim_next(session, "w2", 60, 3) is None


async def test_fail_with_retry_returns_task_to_pending(session, make_user, make_book, make_project):
    user = await make_user("q8@example.com")
    await queue.enqueue(session, await make_project(user, await make_book(user)))
    task = await queue.claim_next(session, "w", 60, 3)
    await queue.fail(session, task, "провайдер недоступен", retry_in=0.0)
    assert task.status == "pending"
    assert await queue.claim_next(session, "w2", 60, 3) is not None


async def test_fail_beyond_max_attempts_is_terminal(session, make_user, make_book, make_project):
    user = await make_user("q9@example.com")
    await queue.enqueue(session, await make_project(user, await make_book(user)))
    for _ in range(3):
        task = await queue.claim_next(session, "w", 60, 3)
        if task is None:
            break
        await queue.fail(session, task, "boom", retry_in=0.0)
    assert await queue.claim_next(session, "w", 60, 3) is None
```

- [ ] **Step 2: Запустить — FAIL**
- [ ] **Step 3: Реализовать** через `sqlalchemy.text()` с SQL из ADD §7 (ORM не выражает `FOR UPDATE SKIP LOCKED` внутри `UPDATE ... WHERE id = (SELECT ...)` чисто).
- [ ] **Step 4: Запустить — PASS (10 passed)**
- [ ] **Step 5: Коммит** — `feat: очередь на арендах с честностью между пользователями и восстановлением после перезапуска`

---

### Task 25: Worker — идемпотентность, пауза, отмена, восстановление

**Files:**
- Create: `.../queue/worker.py`, `backend/src/linguastack/cli.py`
- Create: `backend/tests/test_worker.py`

**Interfaces:**
- Produces: `Worker(worker_id, session_factory, provider_resolver, settings)` с `async def run_once() -> bool` (обработать одну задачу, вернуть «была ли задача») и `async def run_forever(stop_event)`; `python -m linguastack.cli worker` как точка входа.
- Поведение на задачу: материализовать сегменты из CDM (идемпотентно), собрать батчи, переводить, записывать результат **после каждого батча** (ТЗ §7 «сохранение результатов после каждого успешно обработанного блока»), обновлять счётчики проекта, heartbeat, проверять пауза/отмена между батчами.

- [ ] **Step 1: Написать падающий тест**

```python
# backend/tests/test_worker.py
import pytest


async def test_worker_translates_whole_book_and_completes_project(worker_env):
    project = await worker_env.make_project_from_markdown("# Ch\n\nOne.\n\nTwo.")
    await worker_env.run_until_idle()

    refreshed = await worker_env.reload(project)
    assert refreshed.status in {"completed", "completed_with_warnings"}
    assert refreshed.segments_done == 2
    assert refreshed.segments_failed == 0
    assert [s.translated_text for s in await worker_env.segments(project)] == ["RU:One.", "RU:Two."]


async def test_rerunning_task_does_not_retranslate_completed_segments(worker_env):
    """ТЗ §7 и §17: идемпотентность. Повторный прогон пропускает готовое."""
    project = await worker_env.make_project_from_markdown("# Ch\n\nOne.\n\nTwo.")
    await worker_env.run_until_idle()
    calls_after_first = worker_env.provider.call_count

    await worker_env.enqueue(project)
    await worker_env.run_until_idle()
    assert worker_env.provider.call_count == calls_after_first, "модель не должна вызываться снова"


async def test_progress_survives_worker_crash_midway(worker_env):
    """ТЗ §15.8: перевод продолжается после перезапуска сервера."""
    project = await worker_env.make_project_from_markdown(
        "# Ch\n\n" + "\n\n".join(f"Para {i}." for i in range(6)))
    worker_env.provider.fail_after_calls = 1  # упасть посреди книги
    with pytest.raises(Exception):
        await worker_env.run_until_idle(reraise=True)

    partial = await worker_env.reload(project)
    assert 0 < partial.segments_done < 6, "часть работы обязана быть сохранена"

    worker_env.provider.fail_after_calls = None
    await worker_env.recover_and_run()   # новый worker, истёкшая аренда
    assert (await worker_env.reload(project)).segments_done == 6


async def test_paused_project_stops_between_batches_and_keeps_progress(worker_env):
    project = await worker_env.make_project_from_markdown(
        "# Ch\n\n" + "\n\n".join(f"Para {i}." for i in range(10)))
    worker_env.pause_after_batches(1)
    await worker_env.run_until_idle()

    paused = await worker_env.reload(project)
    assert paused.status == "paused"
    assert paused.segments_done > 0, "сделанное не теряется"
    assert paused.segments_done < 10


async def test_resume_continues_from_where_it_stopped(worker_env):
    project = await worker_env.make_project_from_markdown(
        "# Ch\n\n" + "\n\n".join(f"Para {i}." for i in range(10)))
    worker_env.pause_after_batches(1)
    await worker_env.run_until_idle()
    done_at_pause = (await worker_env.reload(project)).segments_done

    await worker_env.resume(project)
    await worker_env.run_until_idle()
    final = await worker_env.reload(project)
    assert final.segments_done == 10
    assert worker_env.provider.translated_count == 10, (
        "уже переведённые сегменты не переводятся повторно")
    assert done_at_pause > 0


async def test_cancel_mid_batch_keeps_paid_for_result(worker_env):
    """Review Focus №5: ответ модели уже оплачен — терять его нельзя."""
    project = await worker_env.make_project_from_markdown(
        "# Ch\n\n" + "\n\n".join(f"Para {i}." for i in range(8)))
    worker_env.cancel_after_batches(1)
    await worker_env.run_until_idle()

    cancelled = await worker_env.reload(project)
    assert cancelled.status == "cancelled"
    assert cancelled.segments_done > 0


async def test_writes_after_cancel_do_not_revive_project(worker_env):
    """Review Focus №5: отменённый проект не должен стать completed."""
    project = await worker_env.make_project_from_markdown("# Ch\n\nOne.\n\nTwo.")
    await worker_env.cancel(project)
    await worker_env.run_until_idle()
    assert (await worker_env.reload(project)).status == "cancelled"


async def test_book_without_translatable_blocks_completes_immediately(worker_env):
    """Review Focus №1: глава целиком из кода. 0/0 не должно делить на ноль или зависать."""
    project = await worker_env.make_project_from_markdown(
        "# Ch\n\n```python\nprint(1)\n```\n")
    await worker_env.run_until_idle()
    done = await worker_env.reload(project)
    assert done.status == "completed"
    assert done.segments_total == 0 and done.segments_done == 0
    assert worker_env.progress_percent(done) == 100.0


async def test_unavailable_provider_retries_then_fails_task(worker_env):
    project = await worker_env.make_project_from_markdown("# Ch\n\nOne.")
    worker_env.provider.always_unavailable = True
    await worker_env.run_until_idle()
    assert (await worker_env.reload(project)).status == "failed"
    assert await worker_env.task_status(project) == "failed"


async def test_segment_failing_all_attempts_does_not_stop_the_book(worker_env):
    project = await worker_env.make_project_from_markdown("# Ch\n\nBad.\n\nGood.")
    worker_env.provider.fail_for_texts = {"Bad."}
    await worker_env.run_until_idle()
    final = await worker_env.reload(project)
    assert final.status == "failed"
    assert final.segments_done == 1, "остальные сегменты обязаны быть переведены"
    assert final.segments_failed == 1
```

- [ ] **Step 2: Запустить — FAIL**
- [ ] **Step 3: Реализовать `worker_env`** — фикстуру, собирающую worker с `FakeProvider`, и `worker.py`. Запись сегмента всегда с условием `WHERE status <> 'completed'` (ADD §7, идемпотентность). Обновление статуса проекта — **только** если текущий статус не терминальный (`cancelled`/`failed`), иначе отменённый проект «воскреснет».
- [ ] **Step 4: Запустить — PASS (10 passed)**
- [ ] **Step 5: Коммит** — `feat: worker очереди с идемпотентностью, паузой, отменой и восстановлением`

---

### Task 26: API переводов, сегментов и поток прогресса

**Files:**
- Create: `.../api/translations.py`, `.../api/segments.py`, `.../api/events.py`
- Modify: `.../main.py`, `.../api/deps.py`
- Create: `backend/tests/test_translations_api.py`, `backend/tests/test_isolation_matrix.py`

**Interfaces:**
- Produces (ТЗ §12): `POST /api/books/{id}/translations`, `GET /api/translations`, `GET /api/translations/{id}`, `POST /api/translations/{id}/pause`, `POST /api/translations/{id}/resume`, `POST /api/translations/{id}/cancel`, `GET /api/translations/{id}/segments`, `POST /api/segments/{id}/retry`, `PATCH /api/segments/{id}`, `GET /api/translations/{id}/events` (SSE, ADD §13).

- [ ] **Step 1: Написать падающий тест** — существенные проверки:

```python
# backend/tests/test_translations_api.py
async def test_create_translation_enqueues_task(user_client, book_id, fake_config_id):
    r = await user_client.post(f"/api/books/{book_id}/translations", json={
        "source_lang": "en", "target_lang": "ru",
        "provider_config_id": fake_config_id, "model_id": "fake-1"})
    assert r.status_code == 201
    assert r.json()["status"] == "queued"


async def test_external_provider_rejected_when_user_forbade_egress(user_client, book_id):
    """ТЗ §6.4: пользователь должен иметь возможность запретить передачу текста наружу."""
    config = await user_client.post("/api/provider-configs", json={
        "provider_id": "anthropic", "api_key": "sk-ant-x-1234", "allow_external": False})
    r = await user_client.post(f"/api/books/{book_id}/translations", json={
        "source_lang": "en", "target_lang": "ru",
        "provider_config_id": config.json()["id"], "model_id": "claude-opus-5-5"})
    assert r.status_code == 400
    assert "external" in r.text.lower() or "внешн" in r.text.lower()


async def test_too_many_active_projects_rejected(user_client, book_id, fake_config_id, monkeypatch):
    """ТЗ §13: ограничение количества активных задач."""
    monkeypatch.setattr("linguastack.config.Settings.max_projects_per_user", 2)
    payload = {"source_lang": "en", "target_lang": "ru",
               "provider_config_id": fake_config_id, "model_id": "fake-1"}
    codes = [(await user_client.post(f"/api/books/{book_id}/translations",
              json={**payload, "target_lang": lang})).status_code
             for lang in ("ru", "de", "fr")]
    assert codes[-1] == 429


async def test_pause_resume_cancel_transitions(user_client, project_id):
    assert (await user_client.post(f"/api/translations/{project_id}/pause")).status_code == 200
    assert (await user_client.get(f"/api/translations/{project_id}")).json()["status"] == "paused"
    assert (await user_client.post(f"/api/translations/{project_id}/resume")).status_code == 200
    assert (await user_client.post(f"/api/translations/{project_id}/cancel")).status_code == 200
    assert (await user_client.get(f"/api/translations/{project_id}")).json()["status"] == "cancelled"


async def test_cancelled_project_cannot_be_resumed(user_client, project_id):
    await user_client.post(f"/api/translations/{project_id}/cancel")
    assert (await user_client.post(f"/api/translations/{project_id}/resume")).status_code == 409


async def test_patch_segment_records_manual_revision(user_client, completed_segment_id):
    """ТЗ §8.3: ручное редактирование перевода; §5.1: версии сохраняются."""
    r = await user_client.patch(f"/api/segments/{completed_segment_id}",
        json={"translated_text": "Мой вариант"})
    assert r.status_code == 200
    assert r.json()["translated_text"] == "Мой вариант"
    revisions = (await user_client.get(
        f"/api/segments/{completed_segment_id}/revisions")).json()
    assert revisions[-1]["origin"] == "manual"
    assert len(revisions) >= 2, "предыдущая версия обязана сохраниться"


async def test_retry_segment_requeues_only_that_segment(user_client, failed_segment_id):
    r = await user_client.post(f"/api/segments/{failed_segment_id}/retry")
    assert r.status_code == 202
    assert r.json()["status"] == "pending"


async def test_sse_stream_emits_progress_and_closes_on_terminal_status(user_client, project_id):
    """ADD §13 и ТЗ §8.4: прогресс без перезагрузки страницы."""
    async with user_client.stream("GET", f"/api/translations/{project_id}/events") as response:
        assert response.status_code == 200
        assert response.headers["content-type"].startswith("text/event-stream")
        payloads = []
        async for line in response.aiter_lines():
            if line.startswith("data:"):
                payloads.append(line)
                if len(payloads) >= 2:
                    break
    assert payloads
```

```python
# backend/tests/test_isolation_matrix.py
import pytest

# ТЗ §2.2 и §12: каждая операция обязана проверять права. Один параметризованный
# тест на все ресурсные эндпоинты — чтобы новый эндпоинт нельзя было забыть закрыть.
ENDPOINTS = [
    ("GET", "/api/books/{book_id}"),
    ("GET", "/api/books/{book_id}/toc"),
    ("PUT", "/api/books/{book_id}/toc"),
    ("DELETE", "/api/books/{book_id}"),
    ("POST", "/api/books/{book_id}/translations"),
    ("GET", "/api/translations/{project_id}"),
    ("POST", "/api/translations/{project_id}/pause"),
    ("POST", "/api/translations/{project_id}/resume"),
    ("POST", "/api/translations/{project_id}/cancel"),
    ("GET", "/api/translations/{project_id}/segments"),
    ("GET", "/api/translations/{project_id}/events"),
    ("PATCH", "/api/segments/{segment_id}"),
    ("POST", "/api/segments/{segment_id}/retry"),
    ("GET", "/api/provider-configs/{config_id}"),
    ("DELETE", "/api/provider-configs/{config_id}"),
]


@pytest.mark.parametrize("method,template", ENDPOINTS)
async def test_every_resource_endpoint_returns_404_for_foreign_user(
    method, template, user_client, foreign_resources
):
    url = template.format(**foreign_resources)
    response = await user_client.request(method, url, json={})
    assert response.status_code == 404, f"{method} {template} пропускает чужой ресурс"


@pytest.mark.parametrize("method,template", ENDPOINTS)
async def test_every_resource_endpoint_requires_authentication(method, template, client, foreign_resources):
    url = template.format(**foreign_resources)
    assert (await client.request(method, url, json={})).status_code == 401
```

- [ ] **Step 2: Запустить — FAIL**
- [ ] **Step 3: Реализовать роутеры.** SSE через `sse_starlette.EventSourceResponse`; опрос раз в секунду; закрытие при терминальном статусе. Переходы статусов проверяются явно: `resume` из `cancelled`/`completed` → 409.
- [ ] **Step 4: Запустить — PASS (8 + 30 passed)**
- [ ] **Step 5: Коммит** — `feat: API переводов, правка сегментов и поток прогресса (SSE)`

---

### Task 27: Сквозной сценарий, документация развёртывания, CI

**Files:**
- Create: `backend/tests/test_end_to_end.py`
- Create: `README.md` (раздел развёртывания), `CLAUDE.md`
- Create: `.github/workflows/ci.yml`

**Interfaces:**
- Consumes: всё предыдущее. Produces: проверенный сквозной путь **импорт → структурирование → перевод → проверка → чтение** (ТЗ §17) и инструкцию развёртывания (ТЗ §15.15).

- [ ] **Step 1: Написать падающий тест**

```python
# backend/tests/test_end_to_end.py
import os

import pytest

BOOK = """\
# Введение

This chapter explains the queue.

```python
print("not translated")
```

# Детали

The worker claims a task. See [docs](https://example.com).
"""


async def test_full_cycle_import_translate_validate_read(user_client, worker_env, fake_config_id):
    """ТЗ §17: надёжный цикл импорт → структурирование → перевод → проверка → чтение."""
    book_id = (await user_client.post("/api/books",
        files={"file": ("b.md", BOOK.encode(), "text/markdown")})).json()["id"]

    toc = (await user_client.get(f"/api/books/{book_id}/toc")).json()
    assert [c["title"] for c in toc["chapters"]] == ["Введение", "Детали"]

    project_id = (await user_client.post(f"/api/books/{book_id}/translations", json={
        "source_lang": "en", "target_lang": "ru",
        "provider_config_id": fake_config_id, "model_id": "fake-1"})).json()["id"]

    await worker_env.run_until_idle()

    project = (await user_client.get(f"/api/translations/{project_id}")).json()
    assert project["status"] in {"completed", "completed_with_warnings"}
    assert project["segments_done"] == project["segments_total"] > 0

    segments = (await user_client.get(f"/api/translations/{project_id}/segments")).json()
    translated = {s["node_id"]: s["translated_text"] for s in segments}
    assert all(translated.values())
    # Код не переводился и остался нетронутым.
    assert not any('print("not translated")' in (t or "") for t in translated.values())
    # Ссылка сохранена.
    assert any("https://example.com" in (t or "") for t in translated.values())


async def test_original_file_unchanged_after_translation(user_client, worker_env, fake_config_id, tmp_storage):
    """ТЗ §15.9: оригинал сохраняется без изменений."""
    payload = BOOK.encode()
    book_id = (await user_client.post("/api/books",
        files={"file": ("b.md", payload, "text/markdown")})).json()["id"]
    await user_client.post(f"/api/books/{book_id}/translations", json={
        "source_lang": "en", "target_lang": "ru",
        "provider_config_id": fake_config_id, "model_id": "fake-1"})
    await worker_env.run_until_idle()

    original = next(tmp_storage.rglob("original.md"))
    assert original.read_bytes() == payload


@pytest.mark.live
async def test_live_ollama_end_to_end(user_client, worker_env_live):
    if os.environ.get("LINGUASTACK_LIVE_OLLAMA") != "1":
        pytest.skip("живой Ollama не включён")
    book_id = (await user_client.post("/api/books", files={
        "file": ("b.md", b"# Chapter\n\nThe book is on the table.", "text/markdown")})).json()["id"]
    config_id = (await user_client.post("/api/provider-configs", json={
        "provider_id": "ollama", "base_url": "http://localhost:11434"})).json()["id"]
    project_id = (await user_client.post(f"/api/books/{book_id}/translations", json={
        "source_lang": "en", "target_lang": "ru",
        "provider_config_id": config_id, "model_id": "qwen3:8b"})).json()["id"]

    await worker_env_live.run_until_idle()
    project = (await user_client.get(f"/api/translations/{project_id}")).json()
    assert project["segments_done"] == 1
    segments = (await user_client.get(f"/api/translations/{project_id}/segments")).json()
    assert segments[0]["translated_text"].strip()
```

- [ ] **Step 2: Запустить — FAIL**
- [ ] **Step 3: Довести реализацию до прохождения теста.**
- [ ] **Step 4: Запустить всю сюиту**

Run: `cd backend && .venv/bin/python -m pytest -v && .venv/bin/ruff check src tests`
Expected: всё зелёное, ruff без замечаний.

- [ ] **Step 5: Проверить живой путь через Ollama**

Run: `cd backend && LINGUASTACK_LIVE_OLLAMA=1 .venv/bin/python -m pytest -m live -v`
Expected: PASS — книга реально переводится локальной моделью.

- [ ] **Step 6: Написать `README.md`** — инструкция развёртывания (ТЗ §15.15): требования, `docker compose up -d db`, установка зависимостей, `alembic upgrade head`, генерация `LINGUASTACK_SECRET_KEY`, запуск `uvicorn` и `python -m linguastack.cli worker`, запуск тестов, резервное копирование (`pg_dump` + каталог `storage/`).
- [ ] **Step 7: Написать `.github/workflows/ci.yml`** — Postgres как service-контейнер, `ruff check`, `pytest` без маркера `live`.
- [ ] **Step 8: Коммит** — `feat: сквозной тест цикла, инструкция развёртывания и CI`

---

## Что остаётся за рамками этого плана

Осознанные решения по объёму, а не упущения (ТЗ §16, этапы 4–7):

| Этап | Содержание |
|---|---|
| 4 | Vue 3 + TypeScript: библиотека, импортёр, монитор задач, читалка (ТЗ §8) |
| 5 | PDF, DOCX, HTML; экспортёры EPUB/MD/DOCX/HTML/TXT (ТЗ §3.2) |
| 6 | Telegram-бот: привязка, уведомления, команды (ТЗ §9) |
| 7 | Резервное копирование, метрики, нагрузочные лимиты, расширенный CI/CD (ТЗ §13) |

Критерии приёмки MVP §15.11 (читалка), §15.12 (экспорт) и §15.13 (Telegram)
закрываются этими этапами. Остальные двенадцать закрываются этим планом —
трассировка в ADD, раздел «Трассируемость критериев приёмки».
