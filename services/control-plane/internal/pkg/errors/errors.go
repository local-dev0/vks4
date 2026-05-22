package errors

import (
	"errors"
	"fmt"
)

type Code string

const (
	CodeInvalidArgument Code = "invalid_argument"
	CodeUnauthorized    Code = "unauthorized"
	CodeForbidden       Code = "forbidden"
	CodeNotFound        Code = "not_found"
	CodeConflict        Code = "conflict"
	CodeInternal        Code = "internal"
	CodeUnavailable     Code = "unavailable"
)

type AppError struct {
	Code    Code
	Message string
	Cause   error
	Details map[string]any
}

func (e *AppError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *AppError) Unwrap() error { return e.Cause }

func New(code Code, msg string) *AppError       { return &AppError{Code: code, Message: msg} }
func Wrap(code Code, msg string, err error) *AppError {
	return &AppError{Code: code, Message: msg, Cause: err}
}

func As(err error) (*AppError, bool) {
	var ae *AppError
	if errors.As(err, &ae) {
		return ae, true
	}
	return nil, false
}

func HTTPStatus(code Code) int {
	switch code {
	case CodeInvalidArgument:
		return 400
	case CodeUnauthorized:
		return 401
	case CodeForbidden:
		return 403
	case CodeNotFound:
		return 404
	case CodeConflict:
		return 409
	case CodeUnavailable:
		return 503
	default:
		return 500
	}
}
