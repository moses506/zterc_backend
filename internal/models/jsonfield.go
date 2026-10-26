package models

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
)

// JSONField is a generic GORM-compatible column type for storing a slice of
// structs as JSON/JSONB, used for fields like LessonContent.KeyPoints.
type JSONField[T any] []T

func (j JSONField[T]) Value() (driver.Value, error) {
	if j == nil {
		return "[]", nil
	}
	return json.Marshal(j)
}

func (j *JSONField[T]) Scan(value any) error {
	if value == nil {
		*j = nil
		return nil
	}

	var bytes []byte
	switch v := value.(type) {
	case []byte:
		bytes = v
	case string:
		bytes = []byte(v)
	default:
		return errors.New("unsupported type for JSONField scan")
	}

	if len(bytes) == 0 {
		*j = nil
		return nil
	}

	return json.Unmarshal(bytes, j)
}
