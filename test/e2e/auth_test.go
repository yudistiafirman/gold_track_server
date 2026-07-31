package e2e

import (
	"net/http"
	"testing"
)

func TestLogin_ValidCredentials(t *testing.T) {
	resetDB(t)
	u := seedUser(t, "KASIR", true)

	status, resp := doRequest(t, http.MethodPost, "/api/auth/login", map[string]string{
		"email":    u.Email,
		"password": u.Password,
	}, "")
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (resp=%+v)", status, resp)
	}

	var data struct {
		Token string `json:"token"`
		User  struct {
			ID   string `json:"id"`
			Name string `json:"name"`
			Role string `json:"role"`
		} `json:"user"`
	}
	decodeData(t, resp, &data)

	if data.Token == "" {
		t.Fatal("expected non-empty token")
	}
	if data.User.Role != "KASIR" {
		t.Fatalf("expected role KASIR, got %q", data.User.Role)
	}
	if data.User.ID == "" {
		t.Fatal("expected non-empty user id")
	}
}

func TestLogin_WrongPassword(t *testing.T) {
	resetDB(t)
	u := seedUser(t, "KASIR", true)

	status, resp := doRequest(t, http.MethodPost, "/api/auth/login", map[string]string{
		"email":    u.Email,
		"password": "wrong-password",
	}, "")
	if status != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", status)
	}
	if resp.Error == nil || resp.Error.Message != "Email atau password salah" {
		t.Fatalf("expected generic invalid-credentials message, got %+v", resp.Error)
	}
}

func TestLogin_UnknownEmail(t *testing.T) {
	resetDB(t)

	status, resp := doRequest(t, http.MethodPost, "/api/auth/login", map[string]string{
		"email":    "nobody@e2e.test",
		"password": "whatever123",
	}, "")
	if status != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", status)
	}
	if resp.Error == nil || resp.Error.Message != "Email atau password salah" {
		t.Fatalf("expected generic invalid-credentials message, got %+v", resp.Error)
	}
}

func TestLogin_InactiveUser(t *testing.T) {
	resetDB(t)
	u := seedUser(t, "KASIR", false)

	status, resp := doRequest(t, http.MethodPost, "/api/auth/login", map[string]string{
		"email":    u.Email,
		"password": u.Password,
	}, "")
	if status != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", status)
	}
	// Same generic message as wrong-password — must not leak account status.
	if resp.Error == nil || resp.Error.Message != "Email atau password salah" {
		t.Fatalf("expected generic invalid-credentials message, got %+v", resp.Error)
	}
}

func TestLogin_MissingFields(t *testing.T) {
	resetDB(t)

	status, _ := doRequest(t, http.MethodPost, "/api/auth/login", map[string]string{
		"email":    "",
		"password": "",
	}, "")
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", status)
	}
}

func TestLogin_NoTokenRequired(t *testing.T) {
	// Login must stay reachable without a token — a client can't have one yet.
	resetDB(t)
	u := seedUser(t, "KASIR", true)

	status, _ := doRequest(t, http.MethodPost, "/api/auth/login", map[string]string{
		"email":    u.Email,
		"password": u.Password,
	}, "")
	if status != http.StatusOK {
		t.Fatalf("expected login to succeed without a bearer token, got %d", status)
	}
}

func TestLogout_ValidToken(t *testing.T) {
	resetDB(t)
	u := seedUser(t, "KASIR", true)
	token := login(t, u.Email, u.Password)

	status, resp := doRequest(t, http.MethodPost, "/api/auth/logout", nil, token)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (resp=%+v)", status, resp)
	}
}

func TestLogout_NoToken(t *testing.T) {
	resetDB(t)

	status, _ := doRequest(t, http.MethodPost, "/api/auth/logout", nil, "")
	if status != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", status)
	}
}

func TestLogout_TokenReuseAfterLogoutIsRejected(t *testing.T) {
	resetDB(t)
	u := seedUser(t, "KASIR", true)
	token := login(t, u.Email, u.Password)

	if status, _ := doRequest(t, http.MethodPost, "/api/auth/logout", nil, token); status != http.StatusOK {
		t.Fatalf("expected first logout to succeed, got %d", status)
	}

	// Same token again: must now be rejected — proves the server-side blacklist works.
	status, resp := doRequest(t, http.MethodPost, "/api/auth/logout", nil, token)
	if status != http.StatusUnauthorized {
		t.Fatalf("expected 401 on reused token, got %d (resp=%+v)", status, resp)
	}
}
