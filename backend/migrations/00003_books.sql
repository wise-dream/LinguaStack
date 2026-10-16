-- +goose Up
CREATE TABLE books (
    id                 uuid         PRIMARY KEY,
    owner_id           uuid         NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    title              text         NOT NULL,
    author             text         NOT NULL DEFAULT '',
    language           varchar(35)  NOT NULL DEFAULT '',
    source_format      varchar(32)  NOT NULL,
    -- Имя, данное пользователем, — только метаданные; на диске файл лежит
    -- под UUID (ТЗ §2.2).
    original_filename  varchar(255) NOT NULL,
    original_suffix    varchar(16)  NOT NULL,
    original_size      bigint       NOT NULL,
    status             varchar(32)  NOT NULL DEFAULT 'imported',
    -- Счётчики хранятся колонками, чтобы библиотека не читала документы целиком.
    chapters_count     integer      NOT NULL,
    blocks_count       integer      NOT NULL,
    translatable_count integer      NOT NULL,
    cdm                jsonb        NOT NULL,
    issues             jsonb        NOT NULL DEFAULT '[]',
    created_at         timestamptz  NOT NULL DEFAULT now(),
    updated_at         timestamptz  NOT NULL DEFAULT now()
);
CREATE INDEX books_owner_created_idx ON books (owner_id, created_at DESC);

-- +goose Down
DROP TABLE books;
