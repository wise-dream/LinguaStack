-- +goose Up
-- Квоты пользователя (дополнение §18): NULL — значение по умолчанию из
-- настроек. Хранилище: 0 — без предела. last_seen_at — последний визит
-- для админ-панели, обновляется не чаще раза в несколько минут.
ALTER TABLE users
    ADD COLUMN max_active_projects  integer CHECK (max_active_projects > 0),
    ADD COLUMN max_concurrent_tasks integer CHECK (max_concurrent_tasks > 0),
    ADD COLUMN storage_quota_bytes  bigint  CHECK (storage_quota_bytes >= 0),
    ADD COLUMN last_seen_at         timestamptz;

-- +goose Down
ALTER TABLE users
    DROP COLUMN last_seen_at,
    DROP COLUMN storage_quota_bytes,
    DROP COLUMN max_concurrent_tasks,
    DROP COLUMN max_active_projects;
