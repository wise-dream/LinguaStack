-- +goose Up
-- Поиск по книге (ТЗ §8.3): формы слов, несколько слов, фраза — полнотекстовый
-- поиск PostgreSQL; подсказка при опечатке — сходство триграмм (pg_trgm).
-- Поиск всегда внутри одной книги или одного перевода, поэтому индекса GIN
-- нет: строки отбирает первичный ключ, tsvector уже посчитан.
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- Словарь форм слов по коду языка (translation.languages). Языки без
-- словаря в PostgreSQL — simple: только точные слова без учёта регистра.
-- +goose StatementBegin
CREATE FUNCTION ls_search_config(lang text) RETURNS regconfig
LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
    SELECT CASE lower(split_part(coalesce(lang, ''), '-', 1))
        WHEN 'ar' THEN 'arabic'
        WHEN 'da' THEN 'danish'
        WHEN 'de' THEN 'german'
        WHEN 'el' THEN 'greek'
        WHEN 'en' THEN 'english'
        WHEN 'es' THEN 'spanish'
        WHEN 'fi' THEN 'finnish'
        WHEN 'fr' THEN 'french'
        WHEN 'hi' THEN 'hindi'
        WHEN 'hu' THEN 'hungarian'
        WHEN 'hy' THEN 'armenian'
        WHEN 'id' THEN 'indonesian'
        WHEN 'it' THEN 'italian'
        WHEN 'lt' THEN 'lithuanian'
        WHEN 'nl' THEN 'dutch'
        WHEN 'no' THEN 'norwegian'
        WHEN 'pt' THEN 'portuguese'
        WHEN 'ro' THEN 'romanian'
        WHEN 'ru' THEN 'russian'
        WHEN 'sr' THEN 'serbian'
        WHEN 'sv' THEN 'swedish'
        WHEN 'tr' THEN 'turkish'
        ELSE 'simple'
    END::regconfig
$$;
-- +goose StatementEnd

-- Блоки оригинала для поиска. Оригинал после импорта не меняется, строки
-- заполняются при первом поиске по книге; язык (search_config) меняется,
-- когда он становится известен (дополнение §19).
CREATE TABLE book_blocks (
    book_id       uuid      NOT NULL REFERENCES books (id) ON DELETE CASCADE,
    node_id       text      NOT NULL,
    chapter_index int       NOT NULL,
    ordinal       int       NOT NULL,
    text          text      NOT NULL,
    search_config regconfig NOT NULL,
    search_tsv    tsvector  GENERATED ALWAYS AS (to_tsvector(search_config, text)) STORED,
    PRIMARY KEY (book_id, node_id)
);

-- Перевод ищется по формам языка перевода: язык фрагмента ставит триггер
-- из проекта, так его не забудет ни один путь вставки.
ALTER TABLE translation_segments ADD COLUMN search_config regconfig NOT NULL DEFAULT 'simple';

UPDATE translation_segments s SET search_config = ls_search_config(p.target_lang)
  FROM translation_projects p WHERE p.id = s.project_id;

ALTER TABLE translation_segments ADD COLUMN search_tsv tsvector
    GENERATED ALWAYS AS (to_tsvector(search_config, coalesce(translated_text, ''))) STORED;

-- +goose StatementBegin
CREATE FUNCTION ls_segment_search_config() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    NEW.search_config := ls_search_config(
        (SELECT target_lang FROM translation_projects WHERE id = NEW.project_id));
    RETURN NEW;
END
$$;
-- +goose StatementEnd

CREATE TRIGGER translation_segments_search_config
    BEFORE INSERT ON translation_segments
    FOR EACH ROW EXECUTE FUNCTION ls_segment_search_config();

-- +goose Down
DROP TRIGGER translation_segments_search_config ON translation_segments;
DROP FUNCTION ls_segment_search_config();
ALTER TABLE translation_segments DROP COLUMN search_tsv;
ALTER TABLE translation_segments DROP COLUMN search_config;
DROP TABLE book_blocks;
DROP FUNCTION ls_search_config(text);
DROP EXTENSION IF EXISTS pg_trgm;
