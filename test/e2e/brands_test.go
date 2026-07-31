package e2e

import (
	"net/http"
	"testing"
)

type brandDTO struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	IsActive bool   `json:"is_active"`
}

func TestBrands_RequireAuth(t *testing.T) {
	resetDB(t)

	cases := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/brands/"},
		{http.MethodPost, "/api/brands/"},
		{http.MethodGet, "/api/brands/00000000-0000-0000-0000-000000000000"},
		{http.MethodPut, "/api/brands/00000000-0000-0000-0000-000000000000"},
		{http.MethodDelete, "/api/brands/00000000-0000-0000-0000-000000000000"},
	}
	for _, c := range cases {
		status, _ := doRequest(t, c.method, c.path, nil, "")
		if status != http.StatusUnauthorized {
			t.Errorf("%s %s without token: expected 401, got %d", c.method, c.path, status)
		}
	}
}

func TestBrands_NonAdminForbidden(t *testing.T) {
	resetDB(t)
	kasir := seedUser(t, "KASIR", true)
	token := login(t, kasir.Email, kasir.Password)

	status, resp := doRequest(t, http.MethodGet, "/api/brands/", nil, token)
	if status != http.StatusForbidden {
		t.Fatalf("expected 403, got %d (resp=%+v)", status, resp)
	}
}

func TestBrands_CreateListGetUpdateDelete(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	// Create
	status, resp := doRequest(t, http.MethodPost, "/api/brands/", map[string]string{
		"name": "Antam",
	}, adminToken)
	if status != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d (resp=%+v)", status, resp)
	}
	var created brandDTO
	decodeData(t, resp, &created)
	if created.ID == "" {
		t.Fatal("create: expected non-empty public id")
	}
	if !created.IsActive {
		t.Fatal("create: expected is_active=true by default")
	}

	// List includes the new brand
	status, resp = doRequest(t, http.MethodGet, "/api/brands/", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("list: expected 200, got %d", status)
	}
	var list []brandDTO
	decodeData(t, resp, &list)
	if !containsBrandID(list, created.ID) {
		t.Fatalf("list: expected created brand %s in list", created.ID)
	}

	// Get by public id
	status, resp = doRequest(t, http.MethodGet, "/api/brands/"+created.ID, nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("get: expected 200, got %d", status)
	}
	var fetched brandDTO
	decodeData(t, resp, &fetched)
	if fetched.Name != "Antam" {
		t.Fatalf("get: expected matching name, got %q", fetched.Name)
	}

	// Update
	status, resp = doRequest(t, http.MethodPut, "/api/brands/"+created.ID, map[string]any{
		"name":      "Antam Logam Mulia",
		"is_active": true,
	}, adminToken)
	if status != http.StatusOK {
		t.Fatalf("update: expected 200, got %d (resp=%+v)", status, resp)
	}
	var updated brandDTO
	decodeData(t, resp, &updated)
	if updated.Name != "Antam Logam Mulia" {
		t.Fatalf("update: field not applied, got %+v", updated)
	}

	// Delete (soft)
	status, resp = doRequest(t, http.MethodDelete, "/api/brands/"+created.ID, nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("delete: expected 200, got %d (resp=%+v)", status, resp)
	}

	status, resp = doRequest(t, http.MethodGet, "/api/brands/"+created.ID, nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("get after delete: expected 200, got %d", status)
	}
	var deactivated brandDTO
	decodeData(t, resp, &deactivated)
	if deactivated.IsActive {
		t.Fatal("expected is_active=false after delete")
	}
}

func TestBrands_CreateDuplicateNameConflict(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "SUPER_ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	if status, _ := doRequest(t, http.MethodPost, "/api/brands/", map[string]string{"name": "UBS"}, adminToken); status != http.StatusCreated {
		t.Fatalf("first create: expected 201, got %d", status)
	}

	// Same name, different case — must still conflict (case-insensitive unique index).
	status, resp := doRequest(t, http.MethodPost, "/api/brands/", map[string]string{"name": "ubs"}, adminToken)
	if status != http.StatusConflict {
		t.Fatalf("expected 409, got %d (resp=%+v)", status, resp)
	}
}

func TestBrands_GetInvalidIDFormat(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "SUPER_ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	status, resp := doRequest(t, http.MethodGet, "/api/brands/1", nil, adminToken)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400 for non-UUID id, got %d (resp=%+v)", status, resp)
	}
}

func TestBrands_GetNotFound(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "SUPER_ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	status, resp := doRequest(t, http.MethodGet, "/api/brands/00000000-0000-0000-0000-000000000000", nil, adminToken)
	if status != http.StatusNotFound {
		t.Fatalf("expected 404, got %d (resp=%+v)", status, resp)
	}
}

func containsBrandID(brands []brandDTO, id string) bool {
	for _, b := range brands {
		if b.ID == id {
			return true
		}
	}
	return false
}
