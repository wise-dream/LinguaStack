"""ORM-модели. Импортируются здесь, чтобы Alembic видел всю метадату."""

from linguastack.db.models.user import User

__all__ = ["User"]
