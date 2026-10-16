"""Зависимости FastAPI: текущий пользователь и проверка владения ресурсом."""

from typing import Annotated, TypeVar

from fastapi import Depends, HTTPException, Request, status
from sqlalchemy import select
from sqlalchemy.ext.asyncio import AsyncSession

from linguastack.db.base import get_session
from linguastack.db.models.user import User
from linguastack.security.sessions import SESSION_COOKIE, lookup_session_user

T = TypeVar("T")

SessionDep = Annotated[AsyncSession, Depends(get_session)]


async def current_user(request: Request, session: SessionDep) -> User:
    token = request.cookies.get(SESSION_COOKIE)
    if not token:
        raise HTTPException(status.HTTP_401_UNAUTHORIZED, "Требуется аутентификация")
    user = await lookup_session_user(session, token)
    if user is None:
        raise HTTPException(status.HTTP_401_UNAUTHORIZED, "Требуется аутентификация")
    return user


CurrentUser = Annotated[User, Depends(current_user)]


async def owned(model: type[T], resource_id: object, user: User, session: AsyncSession) -> T:
    """Вернуть ресурс, принадлежащий пользователю, иначе 404.

    Единственная точка проверки прав (ТЗ §2.2, ADD §9). Два свойства
    существенны: отдаём 404, а не 403, потому что 403 подтверждает
    существование чужого ресурса; и роль admin здесь обхода не даёт —
    ТЗ §2.1 прямо требует, чтобы доступ к содержимому книг не возникал
    из одной роли.
    """
    row = (
        await session.execute(
            select(model).where(model.id == resource_id, model.owner_id == user.id)
        )
    ).scalar_one_or_none()
    if row is None:
        raise HTTPException(status.HTTP_404_NOT_FOUND, "Не найдено")
    return row
