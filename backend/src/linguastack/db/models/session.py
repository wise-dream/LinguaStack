"""Серверные сессии.

Непрозрачный токен вместо JWT: сессию нужно уметь отзывать (ТЗ §2.1 «выход»),
а cookie работает с SSE без прокидывания токена в query-строку, где он
оказался бы в логах прокси. Обоснование — ADD §8.
"""

import uuid
from datetime import datetime

from sqlalchemy import ForeignKey, String, func
from sqlalchemy.dialects.postgresql import UUID as PgUUID
from sqlalchemy.orm import Mapped, mapped_column, relationship

from linguastack.db.base import Base
from linguastack.db.models.user import User


class UserSession(Base):
    __tablename__ = "user_sessions"

    id: Mapped[uuid.UUID] = mapped_column(
        PgUUID(as_uuid=True), primary_key=True, default=uuid.uuid4
    )
    user_id: Mapped[uuid.UUID] = mapped_column(
        PgUUID(as_uuid=True), ForeignKey("users.id", ondelete="CASCADE"), nullable=False, index=True
    )
    # В БД только sha256 токена: утечка дампа не должна давать готовые сессии.
    token_hash: Mapped[str] = mapped_column(String(64), unique=True, index=True, nullable=False)
    expires_at: Mapped[datetime] = mapped_column(nullable=False)
    created_at: Mapped[datetime] = mapped_column(server_default=func.now(), nullable=False)

    user: Mapped[User] = relationship(lazy="joined")
