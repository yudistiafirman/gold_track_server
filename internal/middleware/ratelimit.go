package middleware

import (
	"net/http"
	"time"

	"github.com/go-chi/httprate"

	"gold-track-be/pkg/apperror"
	"gold-track-be/pkg/response"
)

// LoginRateLimit throttles login attempts per client IP to slow down
// credential stuffing / brute force against POST /api/auth/login, the one
// endpoint that sits outside JWTAuth and accepts a password. 10 attempts per
// minute is generous for a real user mistyping their password.
func LoginRateLimit() func(http.Handler) http.Handler {
	return httprate.Limit(
		10,
		time.Minute,
		httprate.WithKeyFuncs(httprate.KeyByIP),
		httprate.WithLimitHandler(func(w http.ResponseWriter, r *http.Request) {
			response.Error(w, apperror.TooManyRequests("Terlalu banyak percobaan login, coba lagi nanti", nil))
		}),
	)
}
