"""Хеширование паролей (ТЗ §13: безопасное хранение паролей).

Argon2id — алгоритм по умолчанию в argon2-cffi; параметры библиотеки
сознательно не переопределяются, чтобы они обновлялись вместе с ней.
"""

from argon2 import PasswordHasher
from argon2.exceptions import Argon2Error, InvalidHashError, VerifyMismatchError

_hasher = PasswordHasher()


def hash_password(plain: str) -> str:
    return _hasher.hash(plain)


def verify_password(plain: str, hashed: str) -> bool:
    """Проверить пароль.

    Никогда не поднимает исключение: повреждённая или пустая запись в БД
    должна означать «не совпало», а не падение процесса аутентификации.
    """
    if not hashed:
        return False
    try:
        return _hasher.verify(hashed, plain)
    except (VerifyMismatchError, InvalidHashError, Argon2Error, ValueError):
        return False


def needs_rehash(hashed: str) -> bool:
    """Нужно ли перехешировать пароль под текущие параметры библиотеки."""
    try:
        return _hasher.check_needs_rehash(hashed)
    except (InvalidHashError, Argon2Error, ValueError):
        return True
