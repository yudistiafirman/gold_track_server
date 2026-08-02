package middleware

import (
	"net/http"

	"github.com/go-chi/cors"
)

// CORS allows browser-based clients on the configured origins to call the
// API. Auth uses a Bearer token in the Authorization header rather than
// cookies, so credentials are never sent cross-origin and AllowCredentials
// stays false even when origins include "*".
func CORS(allowedOrigins []string) func(http.Handler) http.Handler {
	return cors.Handler(cors.Options{
		AllowedOrigins:   allowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Authorization", "Content-Type"},
		AllowCredentials: false,
		MaxAge:           300,
	})
}
