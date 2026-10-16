"""Фабрика FastAPI-приложения."""

from fastapi import FastAPI

from linguastack import __version__


def create_app() -> FastAPI:
    app = FastAPI(title="LinguaStack", version=__version__)

    @app.get("/api/health")
    async def health() -> dict[str, str]:
        return {"status": "ok"}

    return app


app = create_app()
