"""Регистрация, вход, выход и защита от перебора (ТЗ §2.1, §12, §13)."""

import pytest

PASSWORD = "correct-horse-battery"


async def test_register_creates_account(client):
    response = await client.post(
        "/api/auth/register", json={"email": "u@example.com", "password": PASSWORD}
    )
    assert response.status_code == 201
    assert response.json()["email"] == "u@example.com"
    assert response.json()["role"] == "user"


async def test_register_rejects_duplicate_email(client):
    payload = {"email": "dup@example.com", "password": PASSWORD}
    assert (await client.post("/api/auth/register", json=payload)).status_code == 201
    assert (await client.post("/api/auth/register", json=payload)).status_code == 409


async def test_register_rejects_short_password(client):
    response = await client.post(
        "/api/auth/register", json={"email": "s@example.com", "password": "short"}
    )
    assert response.status_code == 422


async def test_register_rejects_invalid_email(client):
    response = await client.post(
        "/api/auth/register", json={"email": "not-an-email", "password": PASSWORD}
    )
    assert response.status_code == 422


async def test_login_sets_httponly_samesite_cookie(client):
    await client.post("/api/auth/register", json={"email": "c@example.com", "password": PASSWORD})
    response = await client.post(
        "/api/auth/login", json={"email": "c@example.com", "password": PASSWORD}
    )
    assert response.status_code == 200
    set_cookie = response.headers["set-cookie"].lower()
    # httponly закрывает токен от JS, samesite — от CSRF на сторонних сайтах.
    # Значения атрибутов регистронезависимы (RFC 6265), поэтому сравниваем в нижнем.
    assert "httponly" in set_cookie
    assert "samesite=lax" in set_cookie


async def test_me_requires_authentication(client):
    assert (await client.get("/api/me")).status_code == 401


async def test_me_returns_current_user_after_login(client):
    await client.post("/api/auth/register", json={"email": "m@example.com", "password": PASSWORD})
    await client.post("/api/auth/login", json={"email": "m@example.com", "password": PASSWORD})
    response = await client.get("/api/me")
    assert response.status_code == 200
    assert response.json()["email"] == "m@example.com"


async def test_password_never_echoed_back(client):
    secret = "super-secret-password"
    response = await client.post(
        "/api/auth/register", json={"email": "p@example.com", "password": secret}
    )
    assert secret not in response.text
    assert "password_hash" not in response.text


async def test_login_failure_does_not_reveal_account_existence(client):
    missing = await client.post(
        "/api/auth/login", json={"email": "nobody@example.com", "password": PASSWORD}
    )
    await client.post(
        "/api/auth/register", json={"email": "real@example.com", "password": PASSWORD}
    )
    wrong = await client.post(
        "/api/auth/login", json={"email": "real@example.com", "password": "wrong-password-x"}
    )
    assert missing.status_code == wrong.status_code == 401
    assert missing.json() == wrong.json(), "ответ не должен выдавать существование аккаунта"


async def test_login_throttled_after_limit(client):
    """ТЗ §13: защита от перебора паролей."""
    await client.post("/api/auth/register", json={"email": "t@example.com", "password": PASSWORD})
    for _ in range(10):
        await client.post(
            "/api/auth/login", json={"email": "t@example.com", "password": "bad-password-x"}
        )
    # Даже с верным паролем — блокировка, иначе лимит обходится угадыванием.
    blocked = await client.post(
        "/api/auth/login", json={"email": "t@example.com", "password": PASSWORD}
    )
    assert blocked.status_code == 429


async def test_throttle_is_scoped_per_email(client):
    """Перебор одного аккаунта не должен блокировать вход другому."""
    for email in ("a1@example.com", "a2@example.com"):
        await client.post("/api/auth/register", json={"email": email, "password": PASSWORD})
    for _ in range(10):
        await client.post(
            "/api/auth/login", json={"email": "a1@example.com", "password": "bad-password-x"}
        )
    ok = await client.post(
        "/api/auth/login", json={"email": "a2@example.com", "password": PASSWORD}
    )
    assert ok.status_code == 200


async def test_logout_invalidates_session(client):
    await client.post("/api/auth/register", json={"email": "l@example.com", "password": PASSWORD})
    await client.post("/api/auth/login", json={"email": "l@example.com", "password": PASSWORD})
    assert (await client.get("/api/me")).status_code == 200
    assert (await client.post("/api/auth/logout")).status_code == 204
    assert (await client.get("/api/me")).status_code == 401


async def test_session_token_stored_hashed_not_plaintext(client, session):
    """Утечка дампа БД не должна давать готовые к использованию сессии."""
    from sqlalchemy import select

    from linguastack.db.models.session import UserSession

    await client.post("/api/auth/register", json={"email": "h@example.com", "password": PASSWORD})
    response = await client.post(
        "/api/auth/login", json={"email": "h@example.com", "password": PASSWORD}
    )
    token = response.cookies["linguastack_session"]

    stored = (await session.execute(select(UserSession))).scalars().all()
    assert stored, "сессия должна быть записана"
    assert all(row.token_hash != token for row in stored)


async def test_unknown_cookie_is_rejected(client):
    client.cookies.set("linguastack_session", "made-up-token-value")
    assert (await client.get("/api/me")).status_code == 401


@pytest.mark.parametrize("inactive_field", ["is_active"])
async def test_deactivated_user_cannot_authenticate(client, session, inactive_field):
    from sqlalchemy import select

    from linguastack.db.models.user import User

    await client.post("/api/auth/register", json={"email": "d@example.com", "password": PASSWORD})
    user = (
        await session.execute(select(User).where(User.email == "d@example.com"))
    ).scalar_one()
    setattr(user, inactive_field, False)
    await session.flush()

    assert (
        await client.post("/api/auth/login", json={"email": "d@example.com", "password": PASSWORD})
    ).status_code == 401
