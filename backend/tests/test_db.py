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


async def test_sessions_are_isolated_between_tests(session):
    """Данные предыдущих тестов не должны протекать — иначе тесты связаны порядком."""
    found = await session.execute(select(User).where(User.email == "a@example.com"))
    assert found.first() is None


async def test_dict_columns_map_to_jsonb():
    from typing import Any

    from sqlalchemy.dialects.postgresql import JSONB

    from linguastack.db.base import Base

    # CDM-блоки и настройки хранятся в JSONB — решение по Postgres, ADD §0.
    assert Base.type_annotation_map[dict[str, Any]] is JSONB
