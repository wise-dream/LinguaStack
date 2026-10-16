# syntax=docker/dockerfile:1
#
# Один образ на оба процесса: api (по умолчанию) и worker
# (entrypoint /app/worker). Сборка статическая, итоговый образ — distroless
# без оболочки и пакетного менеджера, процесс работает не от root.

FROM golang:1.27.2-bookworm AS build
WORKDIR /src
# Тулчейн уже нужной версии (toolchain в go.mod): ничего не скачивать.
ENV GOTOOLCHAIN=local CGO_ENABLED=0
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    go build -trimpath -ldflags="-s -w" -o /out/ ./cmd/api ./cmd/worker
# Каталог данных создаётся заранее с владельцем nonroot: именованный том
# Docker наследует владельца из образа, иначе запись в него запрещена.
RUN mkdir -p /data/storage

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/ /app/
COPY --from=build --chown=nonroot:nonroot /data /data
ENV LINGUASTACK_STORAGE_ROOT=/data/storage \
    LINGUASTACK_HTTP_ADDR=:8000
USER nonroot:nonroot
EXPOSE 8000
ENTRYPOINT ["/app/api"]
