"""Регистрация, вход, выход, текущий пользователь (ТЗ §12)."""

from fastapi import APIRouter, HTTPException, Request, Response, status
from sqlalchemy import select

from linguastack.api.deps import CurrentUser, SessionDep
from linguastack.api.schemas import LoginRequest, RegisterRequest, UserOut
from linguastack.config import get_settings
from linguastack.db.models.user import User
from linguastack.security.passwords import hash_password, verify_password
from linguastack.security.ratelimit import recent_failures, record_attempt
from linguastack.security.sessions import (
    SESSION_COOKIE,
    SESSION_TTL,
    issue_session,
    revoke_session,
)

router = APIRouter(prefix="/api", tags=["auth"])

# Один и тот же ответ на «нет такого аккаунта» и «неверный пароль»:
# различие в ответах позволяет перечислять существующие адреса.
_INVALID_CREDENTIALS = "Неверный адрес или пароль"


def _client_ip(request: Request) -> str:
    return request.client.host if request.client else "unknown"


@router.post("/auth/register", response_model=UserOut, status_code=status.HTTP_201_CREATED)
async def register(payload: RegisterRequest, session: SessionDep) -> User:
    existing = (
        await session.execute(select(User).where(User.email == payload.email))
    ).scalar_one_or_none()
    if existing is not None:
        raise HTTPException(status.HTTP_409_CONFLICT, "Адрес уже зарегистрирован")

    user = User(
        email=payload.email,
        password_hash=hash_password(payload.password),
        role="user",
    )
    session.add(user)
    await session.flush()
    return user


@router.post("/auth/login", response_model=UserOut)
async def login(
    payload: LoginRequest, request: Request, response: Response, session: SessionDep
) -> User:
    settings = get_settings()
    ip = _client_ip(request)

    # Проверка лимита ДО сверки пароля: иначе лимит обходится угадыванием.
    failures = await recent_failures(session, payload.email, ip, settings.login_window_seconds)
    if failures >= settings.login_max_attempts:
        raise HTTPException(
            status.HTTP_429_TOO_MANY_REQUESTS,
            "Слишком много попыток входа. Повторите позже.",
        )

    user = (
        await session.execute(select(User).where(User.email == payload.email))
    ).scalar_one_or_none()
    password_ok = user is not None and verify_password(payload.password, user.password_hash)

    if user is None or not password_ok or not user.is_active:
        await record_attempt(session, payload.email, ip, succeeded=False)
        raise HTTPException(status.HTTP_401_UNAUTHORIZED, _INVALID_CREDENTIALS)

    await record_attempt(session, payload.email, ip, succeeded=True)
    token = await issue_session(session, user)
    response.set_cookie(
        SESSION_COOKIE,
        token,
        httponly=True,
        samesite="lax",
        secure=settings.session_cookie_secure,
        max_age=int(SESSION_TTL.total_seconds()),
        path="/",
    )
    return user


@router.post("/auth/logout", status_code=status.HTTP_204_NO_CONTENT)
async def logout(request: Request, response: Response, session: SessionDep) -> None:
    token = request.cookies.get(SESSION_COOKIE)
    if token:
        await revoke_session(session, token)
    response.delete_cookie(SESSION_COOKIE, path="/")


@router.get("/me", response_model=UserOut)
async def me(user: CurrentUser) -> User:
    return user
