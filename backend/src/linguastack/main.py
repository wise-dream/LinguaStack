"""Фабрика FastAPI-приложения."""

from fastapi import FastAPI

from linguastack import __version__
from linguastack.api import auth


def create_app() -> FastAPI:
    app = FastAPI(title="LinguaStack", version=__version__)

    app.include_router(auth.router)

    @app.get("/api/health")
    async def health() -> dict[str, str]:
        return {"status": "ok"}

    return app


app = create_app()
