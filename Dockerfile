# syntax=docker/dockerfile:1
#
# Один образ на оба процесса: api (по умолчанию; раздаёт и веб-приложение)
# и worker (entrypoint /app/worker). Контекст сборки — корень репозитория.
# Итоговый образ — distroless без оболочки и пакетного менеджера, процесс
# работает не от root.

FROM node:22.22.2-bookworm-slim AS web
WORKDIR /web
COPY frontend/package.json frontend/package-lock.json frontend/.npmrc ./
RUN --mount=type=cache,target=/root/.npm npm ci --no-audit --no-fund
COPY frontend/ ./
RUN npm run build

FROM golang:1.27.2-bookworm AS build
WORKDIR /src
# Тулчейн уже нужной версии (toolchain в go.mod): ничего не скачивать.
ENV GOTOOLCHAIN=local CGO_ENABLED=0
COPY backend/go.mod backend/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY backend/ ./
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    go build -trimpath -ldflags="-s -w" -o /out/ ./cmd/api ./cmd/worker
# Каталог данных создаётся заранее с владельцем nonroot: именованный том
# Docker наследует владельца из образа, иначе запись в него запрещена.
RUN mkdir -p /data/storage

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/ /app/
COPY --from=web /web/dist /app/web
COPY --from=build --chown=nonroot:nonroot /data /data
ENV LINGUASTACK_STORAGE_ROOT=/data/storage \
    LINGUASTACK_HTTP_ADDR=:8000 \
    LINGUASTACK_WEB_ROOT=/app/web
USER nonroot:nonroot
EXPOSE 8000
ENTRYPOINT ["/app/api"]
