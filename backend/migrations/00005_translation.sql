-- +goose Up
CREATE TABLE translation_projects (
    id                 uuid             PRIMARY KEY,
    owner_id           uuid             NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    book_id            uuid             NOT NULL REFERENCES books (id) ON DELETE CASCADE,
    -- RESTRICT: удалить конфигурацию, которой пользуется проект, нельзя —
    -- проект остался бы без модели посреди книги.
    provider_config_id uuid             NOT NULL REFERENCES provider_configs (id) ON DELETE RESTRICT,
    source_lang        varchar(16)      NOT NULL,
    target_lang        varchar(16)      NOT NULL,
    model_id           varchar(200)     NOT NULL,
    style              varchar(32)      NOT NULL DEFAULT 'neutral',
    determinism        varchar(16)      NOT NULL DEFAULT 'strict',
    glossary           jsonb            NOT NULL DEFAULT '{}',
    status             varchar(32)      NOT NULL,
    segments_total     integer          NOT NULL DEFAULT 0,
    segments_done      integer          NOT NULL DEFAULT 0,
    segments_failed    integer          NOT NULL DEFAULT 0,
    warnings_count     integer          NOT NULL DEFAULT 0,
    cost_usd           double precision NOT NULL DEFAULT 0,
    last_error         text             NOT NULL DEFAULT '',
    created_at         timestamptz      NOT NULL DEFAULT now(),
    updated_at         timestamptz      NOT NULL DEFAULT now(),
    started_at         timestamptz,
    finished_at        timestamptz,
    CONSTRAINT translation_projects_status_check CHECK (status IN
        ('draft', 'queued', 'running', 'paused', 'completed', 'completed_with_warnings', 'failed', 'cancelled'))
);
CREATE INDEX translation_projects_owner_idx ON translation_projects (owner_id, created_at DESC);
CREATE INDEX translation_projects_book_idx ON translation_projects (book_id);

-- Сегмент — один переводимый блок CDM (дополнение §2).
CREATE TABLE translation_segments (
    id              uuid        PRIMARY KEY,
    project_id      uuid        NOT NULL REFERENCES translation_projects (id) ON DELETE CASCADE,
    node_id         varchar(32) NOT NULL,
    chapter_index   integer     NOT NULL DEFAULT 0,
    ordinal         integer     NOT NULL DEFAULT 0,
    block_type      varchar(32) NOT NULL,
    level           integer     NOT NULL DEFAULT 0,
    source_text     text        NOT NULL,
    source_hash     varchar(64) NOT NULL,
    translated_text text,
    status          varchar(16) NOT NULL,
    severity        varchar(16) NOT NULL DEFAULT 'ok',
    findings        jsonb       NOT NULL DEFAULT '[]',
    attempts        integer     NOT NULL DEFAULT 0,
    last_error      text        NOT NULL DEFAULT '',
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT translation_segments_node_key UNIQUE (project_id, node_id),
    CONSTRAINT translation_segments_status_check CHECK (status IN ('pending', 'running', 'completed', 'failed', 'stale')),
    CONSTRAINT translation_segments_severity_check CHECK (severity IN ('ok', 'warning', 'error'))
);
CREATE INDEX translation_segments_status_idx ON translation_segments (project_id, status);
CREATE INDEX translation_segments_order_idx ON translation_segments (project_id, chapter_index, ordinal);

-- История версий: повторный перевод не уничтожает предыдущий (ТЗ §5.1).
CREATE TABLE translation_segment_revisions (
    id              uuid        PRIMARY KEY,
    segment_id      uuid        NOT NULL REFERENCES translation_segments (id) ON DELETE CASCADE,
    translated_text text        NOT NULL,
    origin          varchar(16) NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT translation_segment_revisions_origin_check CHECK (origin IN ('machine', 'manual', 'retry'))
);
CREATE INDEX translation_segment_revisions_segment_idx ON translation_segment_revisions (segment_id, created_at);

-- Очередь на арендах (дополнение §7). Статусы — ровно ТЗ §7; пауза живёт
-- на проекте.
CREATE TABLE translation_tasks (
    id           uuid        PRIMARY KEY,
    project_id   uuid        NOT NULL REFERENCES translation_projects (id) ON DELETE CASCADE,
    owner_id     uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    kind         varchar(32) NOT NULL DEFAULT 'translate',
    status       varchar(16) NOT NULL,
    priority     integer     NOT NULL DEFAULT 0,
    attempts     integer     NOT NULL DEFAULT 0,
    max_attempts integer     NOT NULL,
    locked_by    varchar(200),
    locked_until timestamptz,
    available_at timestamptz NOT NULL DEFAULT now(),
    started_at   timestamptz,
    finished_at  timestamptz,
    last_error   text        NOT NULL DEFAULT '',
    created_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT translation_tasks_status_check CHECK (status IN ('pending', 'running', 'completed', 'failed', 'cancelled'))
);
CREATE INDEX translation_tasks_claim_idx ON translation_tasks (status, priority DESC, created_at);
CREATE INDEX translation_tasks_owner_idx ON translation_tasks (owner_id, status);
-- Не больше одной активной задачи на проект: двойное «Перевести» или гонка
-- двух запросов не поставят книгу в очередь дважды (ТЗ §7).
CREATE UNIQUE INDEX translation_tasks_one_active_idx ON translation_tasks (project_id)
    WHERE status IN ('pending', 'running');

-- +goose Down
DROP TABLE translation_tasks;
DROP TABLE translation_segment_revisions;
DROP TABLE translation_segments;
DROP TABLE translation_projects;
