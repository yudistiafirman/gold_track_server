package e2e

import (
	"net/http"
	"testing"
)

type expenseCategoryDTO struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func createExpenseCategory(t *testing.T, adminToken, name string) expenseCategoryDTO {
	t.Helper()
	status, resp := doRequest(t, http.MethodPost, "/api/expense-categories/", map[string]string{"name": name}, adminToken)
	if status != http.StatusCreated {
		t.Fatalf("create expense category fixture: expected 201, got %d (resp=%+v)", status, resp)
	}
	var c expenseCategoryDTO
	decodeData(t, resp, &c)
	return c
}

func TestExpenseCategories_RequireAuth(t *testing.T) {
	resetDB(t)

	cases := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/expense-categories/"},
		{http.MethodPost, "/api/expense-categories/"},
		{http.MethodGet, "/api/expense-categories/" + nonexistentUUID},
		{http.MethodPut, "/api/expense-categories/" + nonexistentUUID},
		{http.MethodDelete, "/api/expense-categories/" + nonexistentUUID},
	}
	for _, c := range cases {
		status, _ := doRequest(t, c.method, c.path, nil, "")
		if status != http.StatusUnauthorized {
			t.Errorf("%s %s without token: expected 401, got %d", c.method, c.path, status)
		}
	}
}

func TestExpenseCategories_NonAdminForbidden(t *testing.T) {
	resetDB(t)
	kasir := seedUser(t, "KASIR", true)
	token := login(t, kasir.Email, kasir.Password)

	status, resp := doRequest(t, http.MethodGet, "/api/expense-categories/", nil, token)
	if status != http.StatusForbidden {
		t.Fatalf("expected 403, got %d (resp=%+v)", status, resp)
	}
}

func TestExpenseCategories_CreateListGetUpdateDelete(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	status, resp := doRequest(t, http.MethodPost, "/api/expense-categories/", map[string]string{"name": "Listrik"}, adminToken)
	if status != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d (resp=%+v)", status, resp)
	}
	var created expenseCategoryDTO
	decodeData(t, resp, &created)
	if created.ID == "" || created.Name != "Listrik" {
		t.Fatalf("create: unexpected response %+v", created)
	}

	status, resp = doRequest(t, http.MethodGet, "/api/expense-categories/", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("list: expected 200, got %d", status)
	}
	var list []expenseCategoryDTO
	decodeData(t, resp, &list)
	found := false
	for _, c := range list {
		if c.ID == created.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("list: expected created category %s in list", created.ID)
	}

	status, resp = doRequest(t, http.MethodGet, "/api/expense-categories/"+created.ID, nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("get: expected 200, got %d", status)
	}

	status, resp = doRequest(t, http.MethodPut, "/api/expense-categories/"+created.ID, map[string]string{"name": "Listrik & Air"}, adminToken)
	if status != http.StatusOK {
		t.Fatalf("update: expected 200, got %d (resp=%+v)", status, resp)
	}
	var updated expenseCategoryDTO
	decodeData(t, resp, &updated)
	if updated.Name != "Listrik & Air" {
		t.Fatalf("update: field not applied, got %+v", updated)
	}

	// Delete is a real hard delete — unlike categories/brands, there's no
	// is_active column, so the row must actually be gone afterward.
	status, resp = doRequest(t, http.MethodDelete, "/api/expense-categories/"+created.ID, nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("delete: expected 200, got %d (resp=%+v)", status, resp)
	}

	status, resp = doRequest(t, http.MethodGet, "/api/expense-categories/"+created.ID, nil, adminToken)
	if status != http.StatusNotFound {
		t.Fatalf("get after delete: expected 404 (hard delete), got %d (resp=%+v)", status, resp)
	}
}

func TestExpenseCategories_CreateDuplicateNameConflict(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	createExpenseCategory(t, adminToken, "Gaji Karyawan")

	status, resp := doRequest(t, http.MethodPost, "/api/expense-categories/", map[string]string{"name": "Gaji Karyawan"}, adminToken)
	if status != http.StatusConflict {
		t.Fatalf("expected 409, got %d (resp=%+v)", status, resp)
	}
}

func TestExpenseCategories_UpdateDuplicateNameConflict(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	createExpenseCategory(t, adminToken, "Sewa Tempat")
	other := createExpenseCategory(t, adminToken, "Transportasi")

	status, resp := doRequest(t, http.MethodPut, "/api/expense-categories/"+other.ID, map[string]string{"name": "Sewa Tempat"}, adminToken)
	if status != http.StatusConflict {
		t.Fatalf("expected 409, got %d (resp=%+v)", status, resp)
	}
}

func TestExpenseCategories_CreateEmptyName(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	status, resp := doRequest(t, http.MethodPost, "/api/expense-categories/", map[string]string{"name": ""}, adminToken)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (resp=%+v)", status, resp)
	}
}

func TestExpenseCategories_GetNotFound(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	status, resp := doRequest(t, http.MethodGet, "/api/expense-categories/"+nonexistentUUID, nil, adminToken)
	if status != http.StatusNotFound {
		t.Fatalf("expected 404, got %d (resp=%+v)", status, resp)
	}
}

func TestExpenseCategories_GetInvalidIDFormat(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	status, resp := doRequest(t, http.MethodGet, "/api/expense-categories/1", nil, adminToken)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (resp=%+v)", status, resp)
	}
}

func TestExpenseCategories_DeleteInUseRejected(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	category := createExpenseCategory(t, adminToken, "Wifi/Internet")

	status, resp := doRequest(t, http.MethodPost, "/api/expenses/", map[string]any{
		"category_id":  category.ID,
		"amount":       500000,
		"description":  "Bulan Juli",
		"expense_date": "2026-07-01",
	}, adminToken)
	if status != http.StatusCreated {
		t.Fatalf("create expense fixture: expected 201, got %d (resp=%+v)", status, resp)
	}

	status, resp = doRequest(t, http.MethodDelete, "/api/expense-categories/"+category.ID, nil, adminToken)
	if status != http.StatusConflict {
		t.Fatalf("expected 409, got %d (resp=%+v)", status, resp)
	}

	// The category must still exist — a rejected delete must not partially apply.
	status, resp = doRequest(t, http.MethodGet, "/api/expense-categories/"+category.ID, nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("expected category still present after rejected delete, got %d (resp=%+v)", status, resp)
	}
}

func TestExpenseCategories_DeleteNotFound(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	status, resp := doRequest(t, http.MethodDelete, "/api/expense-categories/"+nonexistentUUID, nil, adminToken)
	if status != http.StatusNotFound {
		t.Fatalf("expected 404, got %d (resp=%+v)", status, resp)
	}
}
