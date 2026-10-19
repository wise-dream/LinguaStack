-- +goose Up
-- Telegram (ТЗ §9, §11 TelegramBinding; дополнение §17). Один Telegram —
-- один аккаунт и наоборот; код привязки хранится хешем; уведомления
-- пишет worker, доставляет процесс bot.
CREATE TABLE telegram_bindings (
    user_id          uuid        PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    telegram_user_id bigint      NOT NULL UNIQUE,
    chat_id          bigint      NOT NULL,
    username         text        NOT NULL DEFAULT '',
    notify_chapters  boolean     NOT NULL DEFAULT true,
    notify_completed boolean     NOT NULL DEFAULT true,
    notify_errors    boolean     NOT NULL DEFAULT true,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE telegram_link_tokens (
    token_hash bytea       PRIMARY KEY,
    user_id    uuid        NOT NULL UNIQUE REFERENCES users (id) ON DELETE CASCADE,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE telegram_notifications (
    id            uuid        PRIMARY KEY,
    user_id       uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    project_id    uuid        NOT NULL REFERENCES translation_projects (id) ON DELETE CASCADE,
    kind          varchar(16) NOT NULL,
    chapter_index integer,
    dedupe_key    text        NOT NULL UNIQUE,
    status        varchar(16) NOT NULL DEFAULT 'pending',
    attempts      integer     NOT NULL DEFAULT 0,
    available_at  timestamptz NOT NULL DEFAULT now(),
    last_error    text        NOT NULL DEFAULT '',
    created_at    timestamptz NOT NULL DEFAULT now(),
    sent_at       timestamptz,
    CONSTRAINT telegram_notifications_kind_check CHECK (kind IN ('chapter', 'completed', 'failed')),
    CONSTRAINT telegram_notifications_status_check CHECK (status IN ('pending', 'sent', 'skipped', 'failed'))
);

CREATE INDEX telegram_notifications_pending_idx ON telegram_notifications (available_at) WHERE status = 'pending';

-- +goose Down
DROP TABLE telegram_notifications;
DROP TABLE telegram_link_tokens;
DROP TABLE telegram_bindings;
