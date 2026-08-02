package e2e

import (
	"net/http"
	"testing"
)

type customerDTO struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Phone    string `json:"phone"`
	Email    string `json:"email"`
	IDType   string `json:"id_type"`
	IDNumber string `json:"id_number"`
	Address  string `json:"address"`
	Notes    string `json:"notes"`
	IsActive bool   `json:"is_active"`
}

type customerListDTO struct {
	Items      []customerDTO `json:"items"`
	Pagination paginationDTO `json:"pagination"`
}

func createCustomer(t *testing.T, token string, body map[string]any) customerDTO {
	t.Helper()
	if _, ok := body["phone"]; !ok {
		body["phone"] = "081200000000"
	}
	status, resp := doRequest(t, http.MethodPost, "/api/customers/", body, token)
	if status != http.StatusCreated {
		t.Fatalf("create customer fixture: expected 201, got %d (resp=%+v)", status, resp)
	}
	var c customerDTO
	decodeData(t, resp, &c)
	return c
}

func TestCustomers_CreateRequiresAuth(t *testing.T) {
	resetDB(t)

	status, _ := doRequest(t, http.MethodPost, "/api/customers/", map[string]any{"name": "Budi"}, "")
	if status != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", status)
	}
}

func TestCustomers_ListRequiresAuth(t *testing.T) {
	resetDB(t)

	status, _ := doRequest(t, http.MethodGet, "/api/customers/", nil, "")
	if status != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", status)
	}
}

func TestCustomers_GetRequiresAuth(t *testing.T) {
	resetDB(t)

	status, _ := doRequest(t, http.MethodGet, "/api/customers/"+nonexistentUUID, nil, "")
	if status != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", status)
	}
}

func TestCustomers_KasirCanCreateAndRead(t *testing.T) {
	resetDB(t)
	kasir := seedUser(t, "KASIR", true)
	kasirToken := login(t, kasir.Email, kasir.Password)

	created := createCustomer(t, kasirToken, map[string]any{"name": "Budi Santoso"})

	status, resp := doRequest(t, http.MethodGet, "/api/customers/", nil, kasirToken)
	if status != http.StatusOK {
		t.Fatalf("kasir list: expected 200, got %d (resp=%+v)", status, resp)
	}

	status, resp = doRequest(t, http.MethodGet, "/api/customers/"+created.ID, nil, kasirToken)
	if status != http.StatusOK {
		t.Fatalf("kasir get: expected 200, got %d (resp=%+v)", status, resp)
	}
}

func TestCustomers_KasirCannotUpdateOrDelete(t *testing.T) {
	resetDB(t)
	kasir := seedUser(t, "KASIR", true)
	kasirToken := login(t, kasir.Email, kasir.Password)
	created := createCustomer(t, kasirToken, map[string]any{"name": "Budi Santoso"})

	status, resp := doRequest(t, http.MethodPut, "/api/customers/"+created.ID, map[string]any{"name": "Budi Baru"}, kasirToken)
	if status != http.StatusForbidden {
		t.Fatalf("kasir update: expected 403, got %d (resp=%+v)", status, resp)
	}

	status, resp = doRequest(t, http.MethodDelete, "/api/customers/"+created.ID, nil, kasirToken)
	if status != http.StatusForbidden {
		t.Fatalf("kasir delete: expected 403, got %d (resp=%+v)", status, resp)
	}
}

func TestCustomers_CreateListGetUpdateDelete(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	// Create with only name+phone — the rest stay optional.
	status, resp := doRequest(t, http.MethodPost, "/api/customers/", map[string]any{
		"name":  "Budi Santoso",
		"phone": "081200000001",
	}, adminToken)
	if status != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d (resp=%+v)", status, resp)
	}
	var created customerDTO
	decodeData(t, resp, &created)
	if created.ID == "" {
		t.Fatal("create: expected non-empty public id")
	}
	if !created.IsActive {
		t.Fatal("create: expected is_active=true by default")
	}
	if created.Phone != "081200000001" {
		t.Fatalf("create: expected phone to be set, got %+v", created)
	}
	if created.Email != "" || created.IDType != "" || created.IDNumber != "" || created.Address != "" || created.Notes != "" {
		t.Fatalf("create: expected empty optional fields, got %+v", created)
	}

	// List includes the new customer
	status, resp = doRequest(t, http.MethodGet, "/api/customers/", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("list: expected 200, got %d", status)
	}
	var list customerListDTO
	decodeData(t, resp, &list)
	found := false
	for _, c := range list.Items {
		if c.ID == created.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("list: expected created customer %s in list", created.ID)
	}

	// Get by public id
	status, resp = doRequest(t, http.MethodGet, "/api/customers/"+created.ID, nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("get: expected 200, got %d", status)
	}
	var fetched customerDTO
	decodeData(t, resp, &fetched)
	if fetched.Name != "Budi Santoso" {
		t.Fatalf("get: expected matching name, got %q", fetched.Name)
	}

	// Update
	status, resp = doRequest(t, http.MethodPut, "/api/customers/"+created.ID, map[string]any{
		"name":      "Budi Santoso Jr.",
		"phone":     "081234567890",
		"email":     "budi@example.com",
		"id_type":   "KTP",
		"id_number": "3201010101010001",
		"address":   "Jl. Melati No. 5",
		"notes":     "Pelanggan setia",
		"is_active": true,
	}, adminToken)
	if status != http.StatusOK {
		t.Fatalf("update: expected 200, got %d (resp=%+v)", status, resp)
	}
	var updated customerDTO
	decodeData(t, resp, &updated)
	if updated.Name != "Budi Santoso Jr." || updated.Phone != "081234567890" || updated.Email != "budi@example.com" ||
		updated.IDType != "KTP" || updated.IDNumber != "3201010101010001" || updated.Address != "Jl. Melati No. 5" ||
		updated.Notes != "Pelanggan setia" {
		t.Fatalf("update: fields not applied, got %+v", updated)
	}

	// Delete (soft)
	status, resp = doRequest(t, http.MethodDelete, "/api/customers/"+created.ID, nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("delete: expected 200, got %d (resp=%+v)", status, resp)
	}

	status, resp = doRequest(t, http.MethodGet, "/api/customers/"+created.ID, nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("get after delete: expected 200, got %d", status)
	}
	var deactivated customerDTO
	decodeData(t, resp, &deactivated)
	if deactivated.IsActive {
		t.Fatal("expected is_active=false after delete")
	}
}

func TestCustomers_CreateMissingName(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	status, resp := doRequest(t, http.MethodPost, "/api/customers/", map[string]any{}, adminToken)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (resp=%+v)", status, resp)
	}
}

func TestCustomers_CreateInvalidIDType(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	status, resp := doRequest(t, http.MethodPost, "/api/customers/", map[string]any{
		"name":    "Budi Santoso",
		"phone":   "081200000001",
		"id_type": "NPWP",
	}, adminToken)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (resp=%+v)", status, resp)
	}
}

func TestCustomers_CreateMissingPhone(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	status, resp := doRequest(t, http.MethodPost, "/api/customers/", map[string]any{
		"name": "Budi Santoso",
	}, adminToken)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (resp=%+v)", status, resp)
	}
}

func TestCustomers_ListSearchByNameOrPhone(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	createCustomer(t, adminToken, map[string]any{"name": "Budi Santoso", "phone": "081111111111"})
	createCustomer(t, adminToken, map[string]any{"name": "Siti Aminah", "phone": "082222222222"})

	status, resp := doRequest(t, http.MethodGet, "/api/customers/?search=Budi", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("search by name: expected 200, got %d (resp=%+v)", status, resp)
	}
	var byName customerListDTO
	decodeData(t, resp, &byName)
	if len(byName.Items) != 1 || byName.Items[0].Name != "Budi Santoso" {
		t.Fatalf("search=Budi: expected 1 item Budi Santoso, got %+v", byName.Items)
	}

	status, resp = doRequest(t, http.MethodGet, "/api/customers/?search=082222", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("search by phone: expected 200, got %d (resp=%+v)", status, resp)
	}
	var byPhone customerListDTO
	decodeData(t, resp, &byPhone)
	if len(byPhone.Items) != 1 || byPhone.Items[0].Name != "Siti Aminah" {
		t.Fatalf("search=082222: expected 1 item Siti Aminah, got %+v", byPhone.Items)
	}
}

func TestCustomers_ListPagination(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	createCustomer(t, adminToken, map[string]any{"name": "Pelanggan Satu"})
	createCustomer(t, adminToken, map[string]any{"name": "Pelanggan Dua"})
	createCustomer(t, adminToken, map[string]any{"name": "Pelanggan Tiga"})

	status, resp := doRequest(t, http.MethodGet, "/api/customers/?limit=2&page=1", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("page 1: expected 200, got %d (resp=%+v)", status, resp)
	}
	var page1 customerListDTO
	decodeData(t, resp, &page1)
	if len(page1.Items) != 2 || page1.Pagination.Total != 3 || page1.Pagination.TotalPages != 2 {
		t.Fatalf("page 1: expected 2 items total=3 total_pages=2, got %d items %+v", len(page1.Items), page1.Pagination)
	}

	status, resp = doRequest(t, http.MethodGet, "/api/customers/?limit=2&page=2", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("page 2: expected 200, got %d (resp=%+v)", status, resp)
	}
	var page2 customerListDTO
	decodeData(t, resp, &page2)
	if len(page2.Items) != 1 {
		t.Fatalf("page 2: expected 1 item, got %d", len(page2.Items))
	}
}

func TestCustomers_ListShowsArchivedCustomers(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	created := createCustomer(t, adminToken, map[string]any{"name": "Pelanggan Arsip"})
	status, resp := doRequest(t, http.MethodDelete, "/api/customers/"+created.ID, nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("delete: expected 200, got %d (resp=%+v)", status, resp)
	}

	status, resp = doRequest(t, http.MethodGet, "/api/customers/", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("list: expected 200, got %d (resp=%+v)", status, resp)
	}
	var list customerListDTO
	decodeData(t, resp, &list)
	found := false
	for _, c := range list.Items {
		if c.ID == created.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("expected archived customer to remain visible in list")
	}
}

func TestCustomers_GetInvalidIDFormat(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	status, resp := doRequest(t, http.MethodGet, "/api/customers/1", nil, adminToken)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400 for non-UUID id, got %d (resp=%+v)", status, resp)
	}
}

func TestCustomers_GetNotFound(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	status, resp := doRequest(t, http.MethodGet, "/api/customers/"+nonexistentUUID, nil, adminToken)
	if status != http.StatusNotFound {
		t.Fatalf("expected 404, got %d (resp=%+v)", status, resp)
	}
}
