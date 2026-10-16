package models

import (
	"bytes"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
)

// JSON — значение, хранимое в колонке jsonb.
//
// Свой тип на три метода вместо gorm.io/datatypes: тот тянет за собой
// драйверы других СУБД, а нужен ровно Scan/Value.
type JSON[T any] struct {
	Data T
}

// NewJSON оборачивает значение.
func NewJSON[T any](v T) JSON[T] { return JSON[T]{Data: v} }

var errNULInJSON = errors.New("jsonb не допускает символ U+0000")

// Value сериализует значение для записи.
func (j JSON[T]) Value() (driver.Value, error) {
	raw, err := json.Marshal(j.Data)
	if err != nil {
		return nil, fmt.Errorf("сериализация jsonb: %w", err)
	}
	// PostgreSQL отвергает \u0000 в jsonb. Импортёры такого не пропускают;
	// явная ошибка здесь понятнее сообщения СУБД, если это всё же случится.
	if bytes.Contains(raw, []byte(`\u0000`)) {
		return nil, errNULInJSON
	}
	return string(raw), nil
}

// Scan читает значение из базы.
func (j *JSON[T]) Scan(src any) error {
	var raw []byte
	switch v := src.(type) {
	case nil:
		var zero T
		j.Data = zero
		return nil
	case []byte:
		raw = v
	case string:
		raw = []byte(v)
	default:
		return fmt.Errorf("jsonb: неожиданный тип %T", src)
	}
	return json.Unmarshal(raw, &j.Data)
}

// GormDataType сообщает GORM тип колонки.
func (JSON[T]) GormDataType() string { return "jsonb" }
