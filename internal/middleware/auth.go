package middleware

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"gold-track-be/internal/service"
	"gold-track-be/pkg/apperror"
	"gold-track-be/pkg/response"
)

type contextKey string

const claimsContextKey contextKey = "authClaims"

// JWTAuth verifies the Bearer token on every request through it. Missing,
// malformed, expired, badly-signed, or already-blacklisted tokens are all
// rejected with 401. Verified claims are stored in the request context for
// downstream handlers and RequireRole.
func JWTAuth(authService service.AuthService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, err := BearerToken(r)
			if err != nil {
				response.Error(w, apperror.Unauthorized("token tidak ditemukan", err))
				return
			}

			claims, err := authService.VerifyToken(r.Context(), token)
			if err != nil {
				response.Error(w, err)
				return
			}

			ctx := context.WithValue(r.Context(), claimsContextKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireRole must run after JWTAuth. It rejects requests whose authenticated
// role isn't in the allowed set with 403.
func RequireRole(roles ...string) func(http.Handler) http.Handler {
	allowed := make(map[string]struct{}, len(roles))
	for _, role := range roles {
		allowed[role] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := ClaimsFromContext(r.Context())
			if !ok {
				response.Error(w, apperror.Unauthorized("token tidak ditemukan", nil))
				return
			}

			if _, ok := allowed[claims.Role]; !ok {
				response.Error(w, apperror.Forbidden("Anda tidak memiliki akses untuk aksi ini", nil))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// ClaimsFromContext retrieves the authenticated JWT claims set by JWTAuth.
func ClaimsFromContext(ctx context.Context) (*service.Claims, bool) {
	claims, ok := ctx.Value(claimsContextKey).(*service.Claims)
	return claims, ok
}

// BearerToken extracts the token from an "Authorization: Bearer <token>" header.
func BearerToken(r *http.Request) (string, error) {
	const prefix = "Bearer "
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, prefix) {
		return "", errors.New("missing or malformed authorization header")
	}

	token := strings.TrimSpace(strings.TrimPrefix(header, prefix))
	if token == "" {
		return "", errors.New("empty bearer token")
	}
	return token, nil
}
