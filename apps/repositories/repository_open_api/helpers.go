package repository_open_api

import (
	"errors"
	"fmt"

	"gorm.io/gorm"
)

func wrapErr(op string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrNotFound
	}
	return fmt.Errorf("%s: %w", op, err)
}
