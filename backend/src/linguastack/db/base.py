"""Декларативная база, движок и фабрика сессий."""

from collections.abc import AsyncIterator
from datetime import datetime
from typing import Any

from sqlalchemy import DateTime
from sqlalchemy.dialects.postgresql import JSONB
from sqlalchemy.ext.asyncio import (
    AsyncEngine,
    AsyncSession,
    async_sessionmaker,
    create_async_engine,
)
from sqlalchemy.orm import DeclarativeBase

from linguastack.config import get_settings


class Base(DeclarativeBase):
    """Общая база ORM-моделей.

    `type_annotation_map` задаёт два сквозных решения:
    словари едут в JSONB (CDM-блоки, настройки, findings), а все datetime —
    строго timezone-aware, чтобы сравнения времени аренды задач в очереди
    не зависели от локали сервера.
    """

    type_annotation_map = {
        dict[str, Any]: JSONB,
        datetime: DateTime(timezone=True),
    }


def make_engine(url: str) -> AsyncEngine:
    return create_async_engine(url, pool_pre_ping=True)


def session_factory(engine: AsyncEngine) -> async_sessionmaker[AsyncSession]:
    return async_sessionmaker(engine, expire_on_commit=False)


_engine: AsyncEngine | None = None
_factory: async_sessionmaker[AsyncSession] | None = None


def _get_factory() -> async_sessionmaker[AsyncSession]:
    global _engine, _factory
    if _factory is None:
        _engine = make_engine(get_settings().database_url)
        _factory = session_factory(_engine)
    return _factory


async def get_session() -> AsyncIterator[AsyncSession]:
    """Зависимость FastAPI: сессия на запрос."""
    async with _get_factory()() as session:
        yield session
