"""Шифрование пользовательских API-ключей (ТЗ §6.3, ADD §8).

Ключи провайдеров хранятся в БД только в зашифрованном виде. Клиенту они
не возвращаются никогда — вместо них отдаются `fingerprint` и `last4`,
по которым пользователь узнаёт свой ключ, но восстановить его нельзя.
"""

import hashlib

from cryptography.fernet import Fernet

FINGERPRINT_LENGTH = 8


class SecretBox:
    """Обёртка над Fernet (AES-128-CBC + HMAC, со случайным IV)."""

    def __init__(self, key: str) -> None:
        # Fernet сам валидирует длину и бросает ValueError — неверно
        # настроенный ключ должен обнаруживаться при старте, а не при
        # первой попытке расшифровать чужой секрет.
        self._fernet = Fernet(key)

    def encrypt(self, plain: str) -> str:
        return self._fernet.encrypt(plain.encode("utf-8")).decode("ascii")

    def decrypt(self, token: str) -> str:
        return self._fernet.decrypt(token.encode("ascii")).decode("utf-8")

    @staticmethod
    def fingerprint(plain: str) -> str:
        """Короткий необратимый отпечаток — чтобы отличать ключи в интерфейсе."""
        return hashlib.sha256(plain.encode("utf-8")).hexdigest()[:FINGERPRINT_LENGTH]

    @staticmethod
    def last4(plain: str) -> str:
        return plain[-4:]
