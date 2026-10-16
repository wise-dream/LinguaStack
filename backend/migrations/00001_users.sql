-- +goose Up
CREATE TABLE users (
    id            uuid         PRIMARY KEY,
    email         varchar(320) NOT NULL,
    password_hash varchar(255) NOT NULL,
    role          varchar(16)  NOT NULL DEFAULT 'user',
    is_active     boolean      NOT NULL DEFAULT true,
    created_at    timestamptz  NOT NULL DEFAULT now(),
    CONSTRAINT users_email_key UNIQUE (email),
    -- Для первой версии достаточно ролей user и admin (ТЗ §2.1).
    CONSTRAINT users_role_check CHECK (role IN ('user', 'admin'))
);

-- +goose Down
DROP TABLE users;
