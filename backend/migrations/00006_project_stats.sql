-- +goose Up
-- Статистика провайдера для монитора задач (ТЗ §8.4): сколько вызовов и
-- токенов ушло на перевод. Стоимость уже есть (cost_usd).
ALTER TABLE translation_projects
    ADD COLUMN input_tokens   bigint  NOT NULL DEFAULT 0,
    ADD COLUMN output_tokens  bigint  NOT NULL DEFAULT 0,
    ADD COLUMN provider_calls integer NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE translation_projects
    DROP COLUMN input_tokens,
    DROP COLUMN output_tokens,
    DROP COLUMN provider_calls;
