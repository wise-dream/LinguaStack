-- +goose Up
-- Коды сброса пароля (ТЗ §2.1, дополнение §18): хранится хеш, у аккаунта
-- один действующий код — новый запрос заменяет прежний.
CREATE TABLE password_resets (
    token_hash bytea       PRIMARY KEY,
    user_id    uuid        NOT NULL UNIQUE REFERENCES users (id) ON DELETE CASCADE,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE password_resets;
