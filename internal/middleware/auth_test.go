package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	appmw "gold-track-be/internal/middleware"
	"gold-track-be/internal/service"
)

const testSecret = "test-secret"

type fakeBlacklist struct {
	blacklisted map[string]bool
}

func newFakeBlacklist() *fakeBlacklist {
	return &fakeBlacklist{blacklisted: map[string]bool{}}
}

func (f *fakeBlacklist) Add(_ context.Context, jti string, _ time.Time) error {
	f.blacklisted[jti] = true
	return nil
}

func (f *fakeBlacklist) IsBlacklisted(_ context.Context, jti string) (bool, error) {
	return f.blacklisted[jti], nil
}

func (f *fakeBlacklist) DeleteExpired(_ context.Context) (int64, error) {
	return 0, nil
}

func newAuthService(blacklist *fakeBlacklist) service.AuthService {
	return service.NewAuthService(nil, blacklist, testSecret, time.Hour)
}

func signToken(t *testing.T, role, jti string, exp time.Time) string {
	t.Helper()
	claims := service.Claims{
		UserID: "11111111-1111-1111-1111-111111111111",
		Role:   role,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        jti,
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(exp),
		},
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(testSecret))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return signed
}

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func doRequest(t *testing.T, h http.Handler, authHeader string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestJWTAuth_NoToken(t *testing.T) {
	h := appmw.JWTAuth(newAuthService(newFakeBlacklist()))(okHandler())
	rec := doRequest(t, h, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestJWTAuth_MalformedHeader(t *testing.T) {
	h := appmw.JWTAuth(newAuthService(newFakeBlacklist()))(okHandler())
	rec := doRequest(t, h, "token-without-bearer-prefix")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestJWTAuth_InvalidToken(t *testing.T) {
	h := appmw.JWTAuth(newAuthService(newFakeBlacklist()))(okHandler())
	rec := doRequest(t, h, "Bearer garbage.token.value")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestJWTAuth_ExpiredToken(t *testing.T) {
	h := appmw.JWTAuth(newAuthService(newFakeBlacklist()))(okHandler())
	token := signToken(t, "ADMIN", "jti-expired", time.Now().Add(-time.Hour))
	rec := doRequest(t, h, "Bearer "+token)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestJWTAuth_BlacklistedToken(t *testing.T) {
	blacklist := newFakeBlacklist()
	blacklist.blacklisted["jti-blacklisted"] = true
	h := appmw.JWTAuth(newAuthService(blacklist))(okHandler())

	token := signToken(t, "ADMIN", "jti-blacklisted", time.Now().Add(time.Hour))
	rec := doRequest(t, h, "Bearer "+token)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestJWTAuth_ValidToken(t *testing.T) {
	h := appmw.JWTAuth(newAuthService(newFakeBlacklist()))(okHandler())
	token := signToken(t, "ADMIN", "jti-valid", time.Now().Add(time.Hour))
	rec := doRequest(t, h, "Bearer "+token)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestRequireRole_WrongRoleIsForbidden(t *testing.T) {
	authSvc := newAuthService(newFakeBlacklist())
	h := appmw.JWTAuth(authSvc)(appmw.RequireRole("SUPER_ADMIN", "ADMIN")(okHandler()))

	token := signToken(t, "KASIR", "jti-kasir", time.Now().Add(time.Hour))
	rec := doRequest(t, h, "Bearer "+token)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}

func TestRequireRole_AllowedRolePassesThrough(t *testing.T) {
	authSvc := newAuthService(newFakeBlacklist())
	h := appmw.JWTAuth(authSvc)(appmw.RequireRole("ADMIN", "SUPER_ADMIN")(okHandler()))

	token := signToken(t, "ADMIN", "jti-admin", time.Now().Add(time.Hour))
	rec := doRequest(t, h, "Bearer "+token)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}
