"""Выдача, поиск и отзыв сессий."""

import hashlib
import secrets as stdlib_secrets
from datetime import datetime, timedelta, timezone

from sqlalchemy import delete, select
from sqlalchemy.ext.asyncio import AsyncSession

from linguastack.db.models.session import UserSession
from linguastack.db.models.user import User

SESSION_COOKIE = "linguastack_session"
SESSION_TTL = timedelta(days=30)
_TOKEN_BYTES = 32


def hash_token(token: str) -> str:
    return hashlib.sha256(token.encode("utf-8")).hexdigest()


async def issue_session(session: AsyncSession, user: User) -> str:
    """Создать сессию и вернуть открытый токен (в БД попадает только его хеш)."""
    token = stdlib_secrets.token_urlsafe(_TOKEN_BYTES)
    session.add(
        UserSession(
            user_id=user.id,
            token_hash=hash_token(token),
            expires_at=datetime.now(timezone.utc) + SESSION_TTL,
        )
    )
    await session.flush()
    return token


async def lookup_session_user(session: AsyncSession, token: str) -> User | None:
    """Найти владельца действующей сессии, либо None."""
    row = (
        await session.execute(
            select(UserSession).where(
                UserSession.token_hash == hash_token(token),
                UserSession.expires_at > datetime.now(timezone.utc),
            )
        )
    ).scalar_one_or_none()
    if row is None:
        return None
    # Отключённый аккаунт не должен проходить по ранее выданной сессии.
    if not row.user.is_active:
        return None
    return row.user


async def revoke_session(session: AsyncSession, token: str) -> None:
    await session.execute(
        delete(UserSession).where(UserSession.token_hash == hash_token(token))
    )
    await session.flush()
