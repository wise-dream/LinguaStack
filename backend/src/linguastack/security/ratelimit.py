"""Защита от перебора паролей (ТЗ §13, ADD §8).

Счётчик неудач ведётся по паре (email, ip) в скользящем окне. Проверка
выполняется ДО сверки пароля — иначе лимит обходится угадыванием.
"""

from datetime import datetime, timedelta, timezone

from sqlalchemy import delete, func, select
from sqlalchemy.ext.asyncio import AsyncSession

from linguastack.db.models.login_attempt import LoginAttempt


async def recent_failures(
    session: AsyncSession, email: str, ip: str, window_seconds: int
) -> int:
    since = datetime.now(timezone.utc) - timedelta(seconds=window_seconds)
    return (
        await session.execute(
            select(func.count())
            .select_from(LoginAttempt)
            .where(
                LoginAttempt.email == email,
                LoginAttempt.ip == ip,
                LoginAttempt.succeeded.is_(False),
                LoginAttempt.created_at >= since,
            )
        )
    ).scalar_one()


async def record_attempt(
    session: AsyncSession, email: str, ip: str, *, succeeded: bool
) -> None:
    session.add(LoginAttempt(email=email, ip=ip, succeeded=succeeded))
    if succeeded:
        # Успешный вход снимает накопленные неудачи: иначе законный
        # пользователь однажды окажется заблокированным задним числом.
        await session.execute(
            delete(LoginAttempt).where(
                LoginAttempt.email == email,
                LoginAttempt.ip == ip,
                LoginAttempt.succeeded.is_(False),
            )
        )
    await session.flush()
