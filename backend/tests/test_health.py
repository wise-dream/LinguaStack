from fastapi.testclient import TestClient

from linguastack.main import create_app


def test_health_reports_ok():
    client = TestClient(create_app())
    response = client.get("/api/health")
    assert response.status_code == 200
    assert response.json() == {"status": "ok"}


def test_settings_expose_limits_from_spec():
    from linguastack.config import get_settings

    settings = get_settings()
    # Значения по умолчанию из ADD §10 — лимиты обязаны быть настраиваемыми.
    assert settings.max_upload_bytes == 50 * 1024 * 1024
    assert settings.max_zip_entries == 2000
    assert settings.max_tasks_per_user == 3
    assert settings.max_segment_attempts == 3


def test_secret_key_has_no_baked_in_default():
    """Вшитый в код ключ шифрования рано или поздно уезжает в продакшен."""
    from linguastack.config import Settings

    assert Settings.model_fields["secret_key"].default == ""


def test_require_secret_key_raises_actionable_message_when_unset():
    import pytest

    from linguastack.config import Settings

    settings = Settings(secret_key="")
    with pytest.raises(RuntimeError) as exc:
        settings.require_secret_key()
    # Сообщение обязано говорить, что делать, а не только что сломалось.
    assert "LINGUASTACK_SECRET_KEY" in str(exc.value)
    assert "Fernet.generate_key" in str(exc.value)


def test_require_secret_key_returns_configured_value():
    from linguastack.config import Settings

    assert Settings(secret_key="abc123").require_secret_key() == "abc123"
