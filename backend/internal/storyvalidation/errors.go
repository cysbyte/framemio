package storyvalidation

import (
	"errors"
	"strings"
)

type FieldError struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ValidationError struct {
	Errors []FieldError `json:"errors"`
}

func (e *ValidationError) Error() string {
	if e == nil || len(e.Errors) == 0 {
		return "story plan validation failed"
	}
	messages := make([]string, 0, len(e.Errors))
	for _, fieldErr := range e.Errors {
		if fieldErr.Field != "" {
			messages = append(messages, fieldErr.Field+": "+fieldErr.Message)
			continue
		}
		messages = append(messages, fieldErr.Message)
	}
	return "story plan validation failed: " + strings.Join(messages, "; ")
}

func (e *ValidationError) Add(field, code, message string) {
	if e == nil {
		return
	}
	e.Errors = append(e.Errors, FieldError{Field: field, Code: code, Message: message})
}

func (e *ValidationError) HasErrors() bool {
	return e != nil && len(e.Errors) > 0
}

func AsValidationError(err error) (*ValidationError, bool) {
	var validationError *ValidationError
	if errors.As(err, &validationError) {
		return validationError, true
	}
	return nil, false
}
