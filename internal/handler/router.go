package handler

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	appmw "gold-track-be/internal/middleware"
)

// NewRouter wires global middleware (request id, centralized recovery +
// error response, request logging) and all route registrations.
func NewRouter(logger *slog.Logger, healthHandler *HealthHandler) http.Handler {
	r := chi.NewRouter()

	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(appmw.Recoverer(logger))
	r.Use(appmw.RequestLogger(logger))

	r.Get("/health", healthHandler.Check)

	return r
}
