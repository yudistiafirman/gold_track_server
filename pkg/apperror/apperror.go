// Package apperror defines a single application-wide error type so every
// layer (repository, service, handler) can carry an HTTP status and a
// safe-to-expose message without leaking internal details to clients.
package apperror

import (
	"errors"
	"fmt"
	"net/http"
)

type AppError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Status  int    `json:"-"`
	Err     error  `json:"-"`
}

func (e *AppError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Err)
	}
	return e.Message
}

func (e *AppError) Unwrap() error {
	return e.Err
}

func New(status int, code, message string, err error) *AppError {
	return &AppError{Code: code, Message: message, Status: status, Err: err}
}

func NotFound(message string, err error) *AppError {
	return New(http.StatusNotFound, "NOT_FOUND", message, err)
}

func BadRequest(message string, err error) *AppError {
	return New(http.StatusBadRequest, "BAD_REQUEST", message, err)
}

func Internal(message string, err error) *AppError {
	return New(http.StatusInternalServerError, "INTERNAL_ERROR", message, err)
}

func Unavailable(message string, err error) *AppError {
	return New(http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", message, err)
}

func Unauthorized(message string, err error) *AppError {
	return New(http.StatusUnauthorized, "UNAUTHORIZED", message, err)
}

func Forbidden(message string, err error) *AppError {
	return New(http.StatusForbidden, "FORBIDDEN", message, err)
}

func Conflict(message string, err error) *AppError {
	return New(http.StatusConflict, "CONFLICT", message, err)
}

func UnprocessableEntity(message string, err error) *AppError {
	return New(http.StatusUnprocessableEntity, "UNPROCESSABLE_ENTITY", message, err)
}

// As extracts an *AppError from err, wrapping it as an internal error if it isn't one.
func As(err error) *AppError {
	var appErr *AppError
	if errors.As(err, &appErr) {
		return appErr
	}
	return Internal("internal server error", err)
}
