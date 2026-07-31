package e2e

import (
	"encoding/json"
	"net/http"
	"testing"
)

type userDTO struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Email       string  `json:"email"`
	Role        string  `json:"role"`
	IsActive    bool    `json:"is_active"`
	LastLoginAt *string `json:"last_login_at"`
}

func TestUsers_RequireAuth(t *testing.T) {
	resetDB(t)

	cases := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/users/"},
		{http.MethodPost, "/api/users/"},
		{http.MethodGet, "/api/users/00000000-0000-0000-0000-000000000000"},
		{http.MethodPut, "/api/users/00000000-0000-0000-0000-000000000000"},
		{http.MethodDelete, "/api/users/00000000-0000-0000-0000-000000000000"},
	}
	for _, c := range cases {
		status, _ := doRequest(t, c.method, c.path, nil, "")
		if status != http.StatusUnauthorized {
			t.Errorf("%s %s without token: expected 401, got %d", c.method, c.path, status)
		}
	}
}

func TestUsers_NonSuperAdminForbidden(t *testing.T) {
	resetDB(t)
	kasir := seedUser(t, "KASIR", true)
	token := login(t, kasir.Email, kasir.Password)

	status, resp := doRequest(t, http.MethodGet, "/api/users/", nil, token)
	if status != http.StatusForbidden {
		t.Fatalf("expected 403, got %d (resp=%+v)", status, resp)
	}
}

func TestUsers_CreateListGetUpdateDelete(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "SUPER_ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	// Create
	status, resp := doRequest(t, http.MethodPost, "/api/users/", map[string]string{
		"name":     "Kasir Baru",
		"email":    "kasir-crud@e2e.test",
		"password": "KasirPassword123",
		"role":     "KASIR",
	}, adminToken)
	if status != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d (resp=%+v)", status, resp)
	}
	var created userDTO
	decodeData(t, resp, &created)
	if created.ID == "" {
		t.Fatal("create: expected non-empty public id")
	}
	assertNoPasswordField(t, resp.Data)

	// List includes the new user
	status, resp = doRequest(t, http.MethodGet, "/api/users/", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("list: expected 200, got %d", status)
	}
	var list []userDTO
	decodeData(t, resp, &list)
	if !containsID(list, created.ID) {
		t.Fatalf("list: expected created user %s in list", created.ID)
	}

	// Get by public id
	status, resp = doRequest(t, http.MethodGet, "/api/users/"+created.ID, nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("get: expected 200, got %d", status)
	}
	var fetched userDTO
	decodeData(t, resp, &fetched)
	if fetched.Email != "kasir-crud@e2e.test" {
		t.Fatalf("get: expected matching email, got %q", fetched.Email)
	}

	// Update
	status, resp = doRequest(t, http.MethodPut, "/api/users/"+created.ID, map[string]any{
		"name":      "Kasir Diubah",
		"email":     "kasir-crud@e2e.test",
		"role":      "ADMIN",
		"is_active": true,
	}, adminToken)
	if status != http.StatusOK {
		t.Fatalf("update: expected 200, got %d (resp=%+v)", status, resp)
	}
	var updated userDTO
	decodeData(t, resp, &updated)
	if updated.Name != "Kasir Diubah" || updated.Role != "ADMIN" {
		t.Fatalf("update: fields not applied, got %+v", updated)
	}

	// Delete (soft) then confirm deactivated user can no longer log in
	status, resp = doRequest(t, http.MethodDelete, "/api/users/"+created.ID, nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("delete: expected 200, got %d (resp=%+v)", status, resp)
	}

	status, _ = doRequest(t, http.MethodPost, "/api/auth/login", map[string]string{
		"email":    "kasir-crud@e2e.test",
		"password": "KasirPassword123",
	}, "")
	if status != http.StatusUnauthorized {
		t.Fatalf("expected deactivated user login to fail with 401, got %d", status)
	}
}

func TestUsers_CreateDuplicateEmailConflict(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "SUPER_ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	body := map[string]string{
		"name":     "Dup",
		"email":    "dup@e2e.test",
		"password": "Password123",
		"role":     "KASIR",
	}
	if status, _ := doRequest(t, http.MethodPost, "/api/users/", body, adminToken); status != http.StatusCreated {
		t.Fatalf("first create: expected 201, got %d", status)
	}

	status, resp := doRequest(t, http.MethodPost, "/api/users/", body, adminToken)
	if status != http.StatusConflict {
		t.Fatalf("expected 409, got %d (resp=%+v)", status, resp)
	}
}

func TestUsers_GetInvalidIDFormat(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "SUPER_ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	// Sequential-style id must never resolve to a resource.
	status, resp := doRequest(t, http.MethodGet, "/api/users/1", nil, adminToken)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400 for non-UUID id, got %d (resp=%+v)", status, resp)
	}
}

func TestUsers_GetNotFound(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "SUPER_ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	status, resp := doRequest(t, http.MethodGet, "/api/users/00000000-0000-0000-0000-000000000000", nil, adminToken)
	if status != http.StatusNotFound {
		t.Fatalf("expected 404, got %d (resp=%+v)", status, resp)
	}
}

func TestUsers_CannotDeactivateSelf(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "SUPER_ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	status, resp := doRequest(t, http.MethodGet, "/api/users/", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("list: expected 200, got %d", status)
	}
	var list []userDTO
	decodeData(t, resp, &list)
	if len(list) != 1 {
		t.Fatalf("expected exactly the seeded admin in list, got %d", len(list))
	}
	selfID := list[0].ID

	status, resp = doRequest(t, http.MethodDelete, "/api/users/"+selfID, nil, adminToken)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400 self-deactivation guard, got %d (resp=%+v)", status, resp)
	}
}

func assertNoPasswordField(t *testing.T, raw json.RawMessage) {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("decode raw response: %v", err)
	}
	if _, ok := m["password"]; ok {
		t.Fatal("response must never include a password field")
	}
	if _, ok := m["password_hash"]; ok {
		t.Fatal("response must never include a password_hash field")
	}
}

func containsID(users []userDTO, id string) bool {
	for _, u := range users {
		if u.ID == id {
			return true
		}
	}
	return false
}
