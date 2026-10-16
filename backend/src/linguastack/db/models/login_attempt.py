"""Журнал попыток входа — основа защиты от перебора (ТЗ §13, ADD §8)."""

import uuid
from datetime import datetime

from sqlalchemy import Boolean, String, func
from sqlalchemy.dialects.postgresql import UUID as PgUUID
from sqlalchemy.orm import Mapped, mapped_column

from linguastack.db.base import Base


class LoginAttempt(Base):
    __tablename__ = "login_attempts"

    id: Mapped[uuid.UUID] = mapped_column(
        PgUUID(as_uuid=True), primary_key=True, default=uuid.uuid4
    )
    email: Mapped[str] = mapped_column(String(320), index=True, nullable=False)
    ip: Mapped[str] = mapped_column(String(45), nullable=False)
    succeeded: Mapped[bool] = mapped_column(Boolean, nullable=False)
    created_at: Mapped[datetime] = mapped_column(
        server_default=func.now(), index=True, nullable=False
    )
