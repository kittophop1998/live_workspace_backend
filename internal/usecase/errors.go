package usecase

import (
	"errors"
	"fmt"
)

var (
	ErrNotFound    = errors.New("not found")
	ErrValidation  = errors.New("validation error")
	ErrForbidden   = errors.New("forbidden")
	ErrRevConflict = errors.New("revision conflict")
)

type Error struct {
	Kind    error
	Message string
	Details map[string]any
}

func (e *Error) Error() string { return e.Message }
func (e *Error) Unwrap() error { return e.Kind }

func validation(message string, details map[string]any) error {
	return &Error{Kind: ErrValidation, Message: message, Details: details}
}

func notFound(kind, id string) error {
	return &Error{Kind: ErrNotFound, Message: fmt.Sprintf("%s %q not found", kind, id)}
}
