-- +goose Up
-- Непрозрачные серверные сессии вместо JWT: сессию нужно уметь отзывать
-- (ТЗ §2.1 «выход»). В базе только sha256 токена (дополнение §8).
CREATE TABLE user_sessions (
    id         uuid        PRIMARY KEY,
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash varchar(64) NOT NULL,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT user_sessions_token_hash_key UNIQUE (token_hash)
);
CREATE INDEX user_sessions_user_id_idx    ON user_sessions (user_id);
CREATE INDEX user_sessions_expires_at_idx ON user_sessions (expires_at);

-- Журнал попыток входа — основа защиты от перебора (ТЗ §13).
CREATE TABLE login_attempts (
    id         uuid         PRIMARY KEY,
    email      varchar(320) NOT NULL,
    ip         varchar(45)  NOT NULL,
    succeeded  boolean      NOT NULL,
    created_at timestamptz  NOT NULL DEFAULT now()
);
CREATE INDEX login_attempts_lookup_idx ON login_attempts (email, ip, created_at);

-- +goose Down
DROP TABLE login_attempts;
DROP TABLE user_sessions;
