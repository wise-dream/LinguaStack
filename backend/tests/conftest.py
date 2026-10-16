"""Общие фикстуры.

Тестовая база физически отдельная (`linguastack_test`), а каждый тест работает
во вложенной транзакции, которая откатывается после него. Это даёт тестам
независимость от порядка запуска — иначе данные одного теста ломают соседний.
"""

import pytest
import pytest_asyncio
from sqlalchemy import text
from sqlalchemy.ext.asyncio import async_sessionmaker, create_async_engine

from linguastack.config import get_settings
from linguastack.db.base import Base
from linguastack.db.models import User  # noqa: F401 - регистрирует модели в metadata


def _admin_url(url: str) -> str:
    """URL того же сервера, но к служебной БД postgres — для CREATE DATABASE."""
    return url.rsplit("/", 1)[0] + "/postgres"


def _database_name(url: str) -> str:
    return url.rsplit("/", 1)[1]


@pytest_asyncio.fixture(scope="session")
async def engine():
    settings = get_settings()
    url = settings.test_database_url
    name = _database_name(url)

    # CREATE DATABASE нельзя выполнить внутри транзакции.
    admin = create_async_engine(_admin_url(url), isolation_level="AUTOCOMMIT")
    async with admin.connect() as conn:
        exists = await conn.scalar(
            text("select 1 from pg_database where datname = :n"), {"n": name}
        )
        if not exists:
            await conn.execute(text(f'create database "{name}"'))
    await admin.dispose()

    test_engine = create_async_engine(url, pool_pre_ping=True)
    async with test_engine.begin() as conn:
        await conn.run_sync(Base.metadata.create_all)
    yield test_engine
    await test_engine.dispose()


@pytest_asyncio.fixture
async def session(engine):
    """Сессия в транзакции, которая откатывается после теста."""
    connection = await engine.connect()
    transaction = await connection.begin()
    factory = async_sessionmaker(
        bind=connection,
        expire_on_commit=False,
        join_transaction_mode="create_savepoint",
    )
    async with factory() as db_session:
        yield db_session
    await transaction.rollback()
    await connection.close()


@pytest.fixture
def anyio_backend():
    return "asyncio"


@pytest_asyncio.fixture
async def app(session):
    """Приложение, работающее в транзакции теста.

    get_session переопределён на ту же сессию, что видит тест, поэтому
    записи эндпоинтов и проверки теста смотрят на одни данные, а откат
    в конце теста убирает и то, и другое.
    """
    from linguastack.db.base import get_session
    from linguastack.main import create_app

    application = create_app()

    async def _override_session():
        yield session

    application.dependency_overrides[get_session] = _override_session
    return application


@pytest_asyncio.fixture
async def client(app):
    """Неаутентифицированный HTTP-клиент."""
    from httpx import ASGITransport, AsyncClient

    async with AsyncClient(
        transport=ASGITransport(app=app), base_url="http://test"
    ) as http_client:
        yield http_client
