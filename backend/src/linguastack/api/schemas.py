"""Схемы запросов и ответов.

Схемы ответов не содержат полей с секретами — защита на уровне типа,
а не на внимательности при написании хендлера.
"""

import uuid
from datetime import datetime

from pydantic import BaseModel, ConfigDict, EmailStr, Field

MIN_PASSWORD_LENGTH = 12


class RegisterRequest(BaseModel):
    email: EmailStr
    password: str = Field(min_length=MIN_PASSWORD_LENGTH, max_length=256)


class LoginRequest(BaseModel):
    email: EmailStr
    password: str = Field(max_length=256)


class UserOut(BaseModel):
    model_config = ConfigDict(from_attributes=True)

    id: uuid.UUID
    email: EmailStr
    role: str
    created_at: datetime
