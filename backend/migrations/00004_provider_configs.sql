-- +goose Up
CREATE TABLE provider_configs (
    id                uuid         PRIMARY KEY,
    owner_id          uuid         NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    provider_id       varchar(32)  NOT NULL,
    name              varchar(100) NOT NULL DEFAULT '',
    base_url          text         NOT NULL DEFAULT '',
    -- Ключ API только в зашифрованном виде (AES-256-GCM, дополнение §8).
    secret_ciphertext text         NOT NULL DEFAULT '',
    key_fingerprint   varchar(16)  NOT NULL DEFAULT '',
    key_last4         varchar(16)  NOT NULL DEFAULT '',
    -- Явное согласие на передачу текста внешнему сервису (ТЗ §6.4).
    allow_external    boolean      NOT NULL DEFAULT false,
    created_at        timestamptz  NOT NULL DEFAULT now(),
    updated_at        timestamptz  NOT NULL DEFAULT now(),
    CONSTRAINT provider_configs_kind_check CHECK (provider_id IN ('ollama', 'anthropic', 'openai_compatible'))
);
CREATE INDEX provider_configs_owner_idx ON provider_configs (owner_id, created_at);

-- +goose Down
DROP TABLE provider_configs;
