// Package response provides a consistent JSON envelope for handler responses.
package response

import (
	"encoding/json"
	"net/http"

	"gold-track-be/pkg/apperror"
)

type Envelope struct {
	Success bool   `json:"success"`
	Data    any    `json:"data,omitempty"`
	Error   *ErrBody `json:"error,omitempty"`
}

type ErrBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func JSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(Envelope{Success: status < 400, Data: data})
}

func Error(w http.ResponseWriter, err error) {
	appErr := apperror.As(err)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(appErr.Status)
	_ = json.NewEncoder(w).Encode(Envelope{
		Success: false,
		Error:   &ErrBody{Code: appErr.Code, Message: appErr.Message},
	})
}
