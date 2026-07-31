package e2e

import (
	"net/http"
	"testing"
)

type supplierDTO struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Phone    string `json:"phone"`
	Address  string `json:"address"`
	Notes    string `json:"notes"`
	IsActive bool   `json:"is_active"`
}

type supplierListDTO struct {
	Items      []supplierDTO `json:"items"`
	Pagination paginationDTO `json:"pagination"`
}

func createSupplier(t *testing.T, adminToken string, body map[string]any) supplierDTO {
	t.Helper()
	status, resp := doRequest(t, http.MethodPost, "/api/suppliers/", body, adminToken)
	if status != http.StatusCreated {
		t.Fatalf("create supplier fixture: expected 201, got %d (resp=%+v)", status, resp)
	}
	var s supplierDTO
	decodeData(t, resp, &s)
	return s
}

func TestSuppliers_RequireAuth(t *testing.T) {
	resetDB(t)

	cases := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/suppliers/"},
		{http.MethodPost, "/api/suppliers/"},
		{http.MethodGet, "/api/suppliers/00000000-0000-0000-0000-000000000000"},
		{http.MethodPut, "/api/suppliers/00000000-0000-0000-0000-000000000000"},
		{http.MethodDelete, "/api/suppliers/00000000-0000-0000-0000-000000000000"},
	}
	for _, c := range cases {
		status, _ := doRequest(t, c.method, c.path, nil, "")
		if status != http.StatusUnauthorized {
			t.Errorf("%s %s without token: expected 401, got %d", c.method, c.path, status)
		}
	}
}

func TestSuppliers_NonAdminForbidden(t *testing.T) {
	resetDB(t)
	kasir := seedUser(t, "KASIR", true)
	token := login(t, kasir.Email, kasir.Password)

	status, resp := doRequest(t, http.MethodGet, "/api/suppliers/", nil, token)
	if status != http.StatusForbidden {
		t.Fatalf("expected 403, got %d (resp=%+v)", status, resp)
	}
}

func TestSuppliers_CreateListGetUpdateDelete(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	// Create with only name — other fields optional.
	status, resp := doRequest(t, http.MethodPost, "/api/suppliers/", map[string]any{
		"name": "Toko Emas Makmur",
	}, adminToken)
	if status != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d (resp=%+v)", status, resp)
	}
	var created supplierDTO
	decodeData(t, resp, &created)
	if created.ID == "" {
		t.Fatal("create: expected non-empty public id")
	}
	if !created.IsActive {
		t.Fatal("create: expected is_active=true by default")
	}
	if created.Phone != "" || created.Address != "" || created.Notes != "" {
		t.Fatalf("create: expected empty optional fields, got %+v", created)
	}

	// List includes the new supplier
	status, resp = doRequest(t, http.MethodGet, "/api/suppliers/", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("list: expected 200, got %d", status)
	}
	var list supplierListDTO
	decodeData(t, resp, &list)
	found := false
	for _, s := range list.Items {
		if s.ID == created.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("list: expected created supplier %s in list", created.ID)
	}

	// Get by public id
	status, resp = doRequest(t, http.MethodGet, "/api/suppliers/"+created.ID, nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("get: expected 200, got %d", status)
	}
	var fetched supplierDTO
	decodeData(t, resp, &fetched)
	if fetched.Name != "Toko Emas Makmur" {
		t.Fatalf("get: expected matching name, got %q", fetched.Name)
	}

	// Update
	status, resp = doRequest(t, http.MethodPut, "/api/suppliers/"+created.ID, map[string]any{
		"name":      "Toko Emas Makmur Jaya",
		"phone":     "081234567890",
		"address":   "Jl. Emas No. 1",
		"notes":     "Supplier utama",
		"is_active": true,
	}, adminToken)
	if status != http.StatusOK {
		t.Fatalf("update: expected 200, got %d (resp=%+v)", status, resp)
	}
	var updated supplierDTO
	decodeData(t, resp, &updated)
	if updated.Name != "Toko Emas Makmur Jaya" || updated.Phone != "081234567890" ||
		updated.Address != "Jl. Emas No. 1" || updated.Notes != "Supplier utama" {
		t.Fatalf("update: fields not applied, got %+v", updated)
	}

	// Delete (soft)
	status, resp = doRequest(t, http.MethodDelete, "/api/suppliers/"+created.ID, nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("delete: expected 200, got %d (resp=%+v)", status, resp)
	}

	status, resp = doRequest(t, http.MethodGet, "/api/suppliers/"+created.ID, nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("get after delete: expected 200, got %d", status)
	}
	var deactivated supplierDTO
	decodeData(t, resp, &deactivated)
	if deactivated.IsActive {
		t.Fatal("expected is_active=false after delete")
	}
}

func TestSuppliers_CreateMissingName(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	status, resp := doRequest(t, http.MethodPost, "/api/suppliers/", map[string]any{}, adminToken)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (resp=%+v)", status, resp)
	}
}

func TestSuppliers_ListSearchAndPagination(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	createSupplier(t, adminToken, map[string]any{"name": "Toko Emas Makmur"})
	createSupplier(t, adminToken, map[string]any{"name": "Toko Perak Jaya"})
	createSupplier(t, adminToken, map[string]any{"name": "Toko Emas Sejahtera"})

	status, resp := doRequest(t, http.MethodGet, "/api/suppliers/?search=Emas", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("search: expected 200, got %d (resp=%+v)", status, resp)
	}
	var bySearch supplierListDTO
	decodeData(t, resp, &bySearch)
	if len(bySearch.Items) != 2 {
		t.Fatalf("search=Emas: expected 2 items, got %d (%+v)", len(bySearch.Items), bySearch.Items)
	}

	status, resp = doRequest(t, http.MethodGet, "/api/suppliers/?limit=2&page=1", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("page 1: expected 200, got %d (resp=%+v)", status, resp)
	}
	var page1 supplierListDTO
	decodeData(t, resp, &page1)
	if len(page1.Items) != 2 || page1.Pagination.Total != 3 || page1.Pagination.TotalPages != 2 {
		t.Fatalf("page 1: expected 2 items, total=3, total_pages=2, got %d items, %+v", len(page1.Items), page1.Pagination)
	}

	status, resp = doRequest(t, http.MethodGet, "/api/suppliers/?limit=2&page=2", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("page 2: expected 200, got %d (resp=%+v)", status, resp)
	}
	var page2 supplierListDTO
	decodeData(t, resp, &page2)
	if len(page2.Items) != 1 {
		t.Fatalf("page 2: expected 1 item, got %d", len(page2.Items))
	}
}

func TestSuppliers_GetInvalidIDFormat(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	status, resp := doRequest(t, http.MethodGet, "/api/suppliers/1", nil, adminToken)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400 for non-UUID id, got %d (resp=%+v)", status, resp)
	}
}

func TestSuppliers_GetNotFound(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	status, resp := doRequest(t, http.MethodGet, "/api/suppliers/00000000-0000-0000-0000-000000000000", nil, adminToken)
	if status != http.StatusNotFound {
		t.Fatalf("expected 404, got %d (resp=%+v)", status, resp)
	}
}

func TestSuppliers_DeleteIsSoftDelete(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	created := createSupplier(t, adminToken, map[string]any{"name": "Toko Emas Abadi"})

	status, resp := doRequest(t, http.MethodDelete, "/api/suppliers/"+created.ID, nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("delete: expected 200, got %d (resp=%+v)", status, resp)
	}

	status, resp = doRequest(t, http.MethodGet, "/api/suppliers/"+created.ID, nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("get after delete: expected 200 (row not removed), got %d (resp=%+v)", status, resp)
	}
	var fetched supplierDTO
	decodeData(t, resp, &fetched)
	if fetched.IsActive {
		t.Fatal("expected is_active=false after soft delete")
	}

	// Still visible in list (BE-301: archived suppliers are not hidden from list).
	status, resp = doRequest(t, http.MethodGet, "/api/suppliers/", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("list: expected 200, got %d (resp=%+v)", status, resp)
	}
	var list supplierListDTO
	decodeData(t, resp, &list)
	found := false
	for _, s := range list.Items {
		if s.ID == created.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("expected archived supplier to remain visible in list")
	}
}
