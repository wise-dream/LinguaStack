-- +goose Up
-- Позиция чтения (ТЗ §8.3): одна на пользователя и книгу. Удалённый
-- перевод не ломает позицию — читалка откроет оригинал.
CREATE TABLE reading_positions (
    user_id        uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    book_id        uuid        NOT NULL REFERENCES books (id) ON DELETE CASCADE,
    translation_id uuid        NULL REFERENCES translation_projects (id) ON DELETE SET NULL,
    chapter_index  integer     NOT NULL,
    node_id        text        NOT NULL DEFAULT '',
    mode           text        NOT NULL DEFAULT 'parallel',
    updated_at     timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, book_id)
);

-- +goose Down
DROP TABLE reading_positions;
