package handler_open_api

import (
	"fmt"

	"github.com/go-playground/validator/v10"
)

var validate = validator.New()

type fieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func validateRequest(req any) []fieldError {
	err := validate.Struct(req)
	if err == nil {
		return nil
	}
	var errs []fieldError
	for _, ve := range err.(validator.ValidationErrors) {
		errs = append(errs, fieldError{
			Field:   camelCase(ve.Field()),
			Message: humanizeTag(ve),
		})
	}
	return errs
}

func camelCase(field string) string {
	if len(field) == 0 {
		return field
	}
	return fmt.Sprintf("%c%s", field[0]+32, field[1:])
}

func humanizeTag(fe validator.FieldError) string {
	switch fe.Tag() {
	case "required":
		return "is required"
	case "min":
		return fmt.Sprintf("must be at least %s", fe.Param())
	case "max":
		return fmt.Sprintf("must be at most %s", fe.Param())
	case "oneof":
		return fmt.Sprintf("must be one of: %s", fe.Param())
	case "uuid4":
		return "must be a valid UUID"
	default:
		return fmt.Sprintf("failed %s validation", fe.Tag())
	}
}
