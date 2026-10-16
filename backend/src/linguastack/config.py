"""Настройки приложения.

Все лимиты вынесены сюда, а не зашиты в код: ТЗ §13 требует настраиваемых
ограничений, а конкретные значения по умолчанию зафиксированы в
docs/spec/SPEC-ADDENDUM.md §10.
"""

from functools import lru_cache
from pathlib import Path

from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    model_config = SettingsConfigDict(
        env_prefix="LINGUASTACK_",
        env_file=".env",
        extra="ignore",
    )

    database_url: str = "postgresql+asyncpg://linguastack:linguastack@localhost:5434/linguastack"
    test_database_url: str = (
        "postgresql+asyncpg://linguastack:linguastack@localhost:5434/linguastack_test"
    )
    # Fernet-ключ для пользовательских API-ключей (ADD §8).
    # Намеренно без значения по умолчанию: вшитый в код ключ шифрования рано или
    # поздно уезжает в продакшен. Пустая строка означает «не настроен», и обращение
    # к нему падает с понятным сообщением — см. require_secret_key().
    secret_key: str = ""
    storage_root: Path = Path("./storage")
    ollama_base_url: str = "http://localhost:11434"

    # --- лимиты, ADD §10 ---
    max_upload_bytes: int = 50 * 1024 * 1024
    max_zip_entries: int = 2000
    max_zip_total_bytes: int = 200 * 1024 * 1024
    max_zip_ratio: int = 100
    max_tasks_per_user: int = 3
    max_projects_per_user: int = 20
    max_segment_attempts: int = 3
    login_max_attempts: int = 10
    login_window_seconds: int = 15 * 60

    # --- сегментация и батчинг, ADD §2 ---
    max_batch_segments: int = 20
    max_batch_chars: int = 6000

    # --- очередь, ADD §7 ---
    task_lease_seconds: int = 120

    def require_secret_key(self) -> str:
        """Вернуть ключ шифрования или упасть с инструкцией, как его получить.

        Проверка отложена до момента реального использования: приложение должно
        запускаться, проходить health-check и миграции без ключа, но любая работа
        с пользовательскими секретами без него обязана прерваться.
        """
        if not self.secret_key:
            raise RuntimeError(
                "LINGUASTACK_SECRET_KEY не задан. Сгенерировать: "
                'python -c "from cryptography.fernet import Fernet; '
                'print(Fernet.generate_key().decode())"'
            )
        return self.secret_key


@lru_cache(maxsize=1)
def get_settings() -> Settings:
    return Settings()
