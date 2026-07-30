package middleware

import (
	"log/slog"
	"net/http"

	chimw "github.com/go-chi/chi/v5/middleware"

	"gold-track-be/pkg/apperror"
	"gold-track-be/pkg/response"
)

// Recoverer catches panics anywhere in the handler chain, logs them with the
// stack trace, and responds with a consistent JSON 500 instead of a bare
// connection reset — this is the app's centralized error handling entry point.
func Recoverer(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Error("panic_recovered",
						"request_id", chimw.GetReqID(r.Context()),
						"path", r.URL.Path,
						"panic", rec,
					)
					response.Error(w, apperror.Internal("internal server error", nil))
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
