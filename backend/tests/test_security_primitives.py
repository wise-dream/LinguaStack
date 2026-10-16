import base64

import pytest

from linguastack.security.passwords import hash_password, needs_rehash, verify_password
from linguastack.security.secrets import SecretBox

# Fernet требует РОВНО 32 url-safe base64-байта — строка другой длины падает
# с ValueError ещё в конструкторе.
KEY = base64.urlsafe_b64encode(b"linguastack-test-key-32-bytes!!!").decode()


def test_password_hash_is_argon2id_and_verifies():
    hashed = hash_password("correct horse battery staple")
    assert hashed.startswith("$argon2id$"), "ТЗ §13 требует безопасного хеширования"
    assert verify_password("correct horse battery staple", hashed)
    assert not verify_password("wrong", hashed)


def test_password_hash_is_salted():
    assert hash_password("same") != hash_password("same")


def test_verify_rejects_garbage_hash_without_raising():
    # Повреждённая запись в БД не должна валить процесс аутентификации.
    assert verify_password("anything", "not-a-hash") is False
    assert verify_password("anything", "") is False


def test_needs_rehash_false_for_fresh_hash():
    assert needs_rehash(hash_password("pw")) is False


def test_secret_roundtrip_and_non_reversible_metadata():
    box = SecretBox(KEY)
    api_key = "sk-ant-secret-value-12345"
    token = box.encrypt(api_key)
    assert api_key not in token, "шифртекст не должен содержать открытый ключ"
    assert box.decrypt(token) == api_key
    # Клиенту отдаём только это — ADD §8.
    assert box.last4(api_key) == "2345"
    assert len(box.fingerprint(api_key)) == 8


def test_encryption_is_randomised():
    box = SecretBox(KEY)
    assert box.encrypt("same-value") != box.encrypt("same-value")


def test_decrypt_rejects_tampered_token():
    from cryptography.fernet import InvalidToken

    box = SecretBox(KEY)
    token = box.encrypt("value")
    with pytest.raises(InvalidToken):
        box.decrypt(token[:-4] + "AAAA")


def test_decrypt_rejects_token_from_another_key():
    """Ключ сменили — старые секреты не должны расшифровываться молча."""
    from cryptography.fernet import InvalidToken

    other = base64.urlsafe_b64encode(b"another-test-key-32-bytes-long!!").decode()
    token = SecretBox(KEY).encrypt("value")
    with pytest.raises(InvalidToken):
        SecretBox(other).decrypt(token)


def test_invalid_key_fails_loudly_at_construction():
    with pytest.raises(ValueError):
        SecretBox("too-short")


def test_fingerprint_is_stable_and_differs_per_value():
    box = SecretBox(KEY)
    assert box.fingerprint("abc") == box.fingerprint("abc")
    assert box.fingerprint("abc") != box.fingerprint("abd")
