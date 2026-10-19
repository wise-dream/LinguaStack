-- +goose Up
-- Задача экспорта (ТЗ §11 ExportJob, §12; дополнение §16). Выполняет
-- worker: захват FOR UPDATE SKIP LOCKED с арендой locked_until — упавший
-- worker задачу не теряет. Файл — storage/{user}/{book}/exports/{id}{ext};
-- base_name — имя для скачивания без расширения (расширение известно
-- только после сборки: Markdown с картинками — ZIP).
CREATE TABLE export_jobs (
    id           uuid        PRIMARY KEY,
    owner_id     uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    book_id      uuid        NOT NULL REFERENCES books (id) ON DELETE CASCADE,
    project_id   uuid        NOT NULL REFERENCES translation_projects (id) ON DELETE CASCADE,
    format       varchar(16) NOT NULL,
    mode         varchar(16) NOT NULL,
    status       varchar(16) NOT NULL DEFAULT 'pending',
    base_name    text        NOT NULL,
    file_ext     varchar(8)  NOT NULL DEFAULT '',
    content_type text        NOT NULL DEFAULT '',
    size_bytes   bigint      NOT NULL DEFAULT 0,
    untranslated integer     NOT NULL DEFAULT 0,
    error        text        NOT NULL DEFAULT '',
    locked_until timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    finished_at  timestamptz,
    CONSTRAINT export_jobs_format_check CHECK (format IN ('epub', 'markdown', 'docx', 'html', 'txt')),
    CONSTRAINT export_jobs_mode_check CHECK (mode IN ('translation', 'bilingual')),
    CONSTRAINT export_jobs_status_check CHECK (status IN ('pending', 'running', 'completed', 'failed'))
);

CREATE INDEX export_jobs_book_idx ON export_jobs (book_id, created_at DESC);
CREATE INDEX export_jobs_active_idx ON export_jobs (created_at) WHERE status IN ('pending', 'running');

-- +goose Down
DROP TABLE export_jobs;
