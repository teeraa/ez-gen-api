package repository_open_api

import "errors"

var (
	ErrNotFound    = errors.New("not found")
	ErrConflict    = errors.New("conflict")
)
