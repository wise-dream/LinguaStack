-- +goose Up
-- Тестовый провайдер сквозных тестов интерфейса. Создать конфигурацию с
-- ним приложение позволяет только при LINGUASTACK_E2E_FAKE_PROVIDER=true;
-- схема лишь не мешает этому.
ALTER TABLE provider_configs DROP CONSTRAINT provider_configs_kind_check;
ALTER TABLE provider_configs ADD CONSTRAINT provider_configs_kind_check
    CHECK (provider_id IN ('ollama', 'anthropic', 'openai_compatible', 'fake'));

-- +goose Down
DELETE FROM provider_configs WHERE provider_id = 'fake';
ALTER TABLE provider_configs DROP CONSTRAINT provider_configs_kind_check;
ALTER TABLE provider_configs ADD CONSTRAINT provider_configs_kind_check
    CHECK (provider_id IN ('ollama', 'anthropic', 'openai_compatible'));
