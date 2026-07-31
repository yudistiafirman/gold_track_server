package e2e

import (
	"net/http"
	"testing"
)

type expenseDTO struct {
	ID          string        `json:"id"`
	Category    productRefDTO `json:"category"`
	Amount      float64       `json:"amount"`
	Description string        `json:"description"`
	ExpenseDate string        `json:"expense_date"`
}

type expenseListDTO struct {
	Items      []expenseDTO  `json:"items"`
	Pagination paginationDTO `json:"pagination"`
}

func validExpenseBody(categoryID string, overrides map[string]any) map[string]any {
	body := map[string]any{
		"category_id":  categoryID,
		"amount":       500000,
		"description":  "Tagihan bulanan",
		"expense_date": "2026-07-01",
	}
	for k, v := range overrides {
		body[k] = v
	}
	return body
}

func createExpenseAPI(t *testing.T, adminToken string, body map[string]any) expenseDTO {
	t.Helper()
	status, resp := doRequest(t, http.MethodPost, "/api/expenses/", body, adminToken)
	if status != http.StatusCreated {
		t.Fatalf("create expense fixture: expected 201, got %d (resp=%+v)", status, resp)
	}
	var e expenseDTO
	decodeData(t, resp, &e)
	return e
}

func TestExpenses_RequireAuth(t *testing.T) {
	resetDB(t)

	cases := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/expenses/"},
		{http.MethodPost, "/api/expenses/"},
		{http.MethodGet, "/api/expenses/" + nonexistentUUID},
		{http.MethodPut, "/api/expenses/" + nonexistentUUID},
		{http.MethodDelete, "/api/expenses/" + nonexistentUUID},
	}
	for _, c := range cases {
		status, _ := doRequest(t, c.method, c.path, nil, "")
		if status != http.StatusUnauthorized {
			t.Errorf("%s %s without token: expected 401, got %d", c.method, c.path, status)
		}
	}
}

func TestExpenses_NonAdminForbidden(t *testing.T) {
	resetDB(t)
	kasir := seedUser(t, "KASIR", true)
	token := login(t, kasir.Email, kasir.Password)

	status, resp := doRequest(t, http.MethodGet, "/api/expenses/", nil, token)
	if status != http.StatusForbidden {
		t.Fatalf("expected 403, got %d (resp=%+v)", status, resp)
	}
}

func TestExpenses_CreateGetUpdateDelete(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	category := createExpenseCategory(t, adminToken, "ATK")

	created := createExpenseAPI(t, adminToken, validExpenseBody(category.ID, nil))
	if created.Category.ID != category.ID || created.Amount != 500000 || created.ExpenseDate != "2026-07-01" {
		t.Fatalf("create: unexpected response %+v", created)
	}

	status, resp := doRequest(t, http.MethodGet, "/api/expenses/"+created.ID, nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("get: expected 200, got %d (resp=%+v)", status, resp)
	}
	var fetched expenseDTO
	decodeData(t, resp, &fetched)
	if fetched.Description != "Tagihan bulanan" {
		t.Fatalf("get: unexpected response %+v", fetched)
	}

	otherCategory := createExpenseCategory(t, adminToken, "Transportasi")
	status, resp = doRequest(t, http.MethodPut, "/api/expenses/"+created.ID, validExpenseBody(otherCategory.ID, map[string]any{
		"amount": 750000, "expense_date": "2026-07-15",
	}), adminToken)
	if status != http.StatusOK {
		t.Fatalf("update: expected 200, got %d (resp=%+v)", status, resp)
	}
	var updated expenseDTO
	decodeData(t, resp, &updated)
	if updated.Category.ID != otherCategory.ID || updated.Amount != 750000 || updated.ExpenseDate != "2026-07-15" {
		t.Fatalf("update: field not applied, got %+v", updated)
	}

	status, resp = doRequest(t, http.MethodDelete, "/api/expenses/"+created.ID, nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("delete: expected 200, got %d (resp=%+v)", status, resp)
	}

	status, resp = doRequest(t, http.MethodGet, "/api/expenses/"+created.ID, nil, adminToken)
	if status != http.StatusNotFound {
		t.Fatalf("get after delete: expected 404, got %d (resp=%+v)", status, resp)
	}
}

func TestExpenses_CreateMissingRequiredFields(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	category := createExpenseCategory(t, adminToken, "ATK")

	cases := []struct {
		name string
		body map[string]any
	}{
		{"missing category_id", validExpenseBody("", nil)},
		{"missing amount", validExpenseBody(category.ID, map[string]any{"amount": 0})},
		{"missing expense_date", validExpenseBody(category.ID, map[string]any{"expense_date": ""})},
		{"negative amount", validExpenseBody(category.ID, map[string]any{"amount": -100})},
	}
	for _, c := range cases {
		status, resp := doRequest(t, http.MethodPost, "/api/expenses/", c.body, adminToken)
		if status != http.StatusBadRequest {
			t.Errorf("%s: expected 400, got %d (resp=%+v)", c.name, status, resp)
		}
	}
}

func TestExpenses_CreateBadExpenseDateFormat(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	category := createExpenseCategory(t, adminToken, "ATK")

	status, resp := doRequest(t, http.MethodPost, "/api/expenses/", validExpenseBody(category.ID, map[string]any{"expense_date": "01-07-2026"}), adminToken)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (resp=%+v)", status, resp)
	}
}

func TestExpenses_CreateCategoryNotFound(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	status, resp := doRequest(t, http.MethodPost, "/api/expenses/", validExpenseBody(nonexistentUUID, nil), adminToken)
	if status != http.StatusNotFound {
		t.Fatalf("expected 404, got %d (resp=%+v)", status, resp)
	}
}

func TestExpenses_GetNotFound(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	status, resp := doRequest(t, http.MethodGet, "/api/expenses/"+nonexistentUUID, nil, adminToken)
	if status != http.StatusNotFound {
		t.Fatalf("expected 404, got %d (resp=%+v)", status, resp)
	}
}

func TestExpenses_GetInvalidIDFormat(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	status, resp := doRequest(t, http.MethodGet, "/api/expenses/1", nil, adminToken)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (resp=%+v)", status, resp)
	}
}

func TestExpenses_ListFiltersByCategory(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	categoryA := createExpenseCategory(t, adminToken, "Listrik")
	categoryB := createExpenseCategory(t, adminToken, "Wifi/Internet")

	createExpenseAPI(t, adminToken, validExpenseBody(categoryA.ID, nil))
	createExpenseAPI(t, adminToken, validExpenseBody(categoryB.ID, nil))

	status, resp := doRequest(t, http.MethodGet, "/api/expenses/?category_id="+categoryA.ID, nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (resp=%+v)", status, resp)
	}
	var list expenseListDTO
	decodeData(t, resp, &list)
	if list.Pagination.Total != 1 || list.Items[0].Category.ID != categoryA.ID {
		t.Fatalf("expected only categoryA expense, got %+v", list)
	}
}

func TestExpenses_ListFiltersByDateRange(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	category := createExpenseCategory(t, adminToken, "Sewa Tempat")

	createExpenseAPI(t, adminToken, validExpenseBody(category.ID, map[string]any{"expense_date": "2026-06-15"}))
	createExpenseAPI(t, adminToken, validExpenseBody(category.ID, map[string]any{"expense_date": "2026-07-15"}))
	createExpenseAPI(t, adminToken, validExpenseBody(category.ID, map[string]any{"expense_date": "2026-08-15"}))

	status, resp := doRequest(t, http.MethodGet, "/api/expenses/?date_from=2026-07-01&date_to=2026-07-31", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (resp=%+v)", status, resp)
	}
	var list expenseListDTO
	decodeData(t, resp, &list)
	if list.Pagination.Total != 1 || list.Items[0].ExpenseDate != "2026-07-15" {
		t.Fatalf("expected only the July expense, got %+v", list)
	}

	// Boundary-inclusive check: date_from equal to the expense_date itself.
	status, resp = doRequest(t, http.MethodGet, "/api/expenses/?date_from=2026-07-15&date_to=2026-07-15", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (resp=%+v)", status, resp)
	}
	var boundary expenseListDTO
	decodeData(t, resp, &boundary)
	if boundary.Pagination.Total != 1 {
		t.Fatalf("expected boundary date to be inclusive, got %+v", boundary)
	}

	// Open-ended range: only date_from set.
	status, resp = doRequest(t, http.MethodGet, "/api/expenses/?date_from=2026-08-01", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (resp=%+v)", status, resp)
	}
	var openEnded expenseListDTO
	decodeData(t, resp, &openEnded)
	if openEnded.Pagination.Total != 1 || openEnded.Items[0].ExpenseDate != "2026-08-15" {
		t.Fatalf("expected only the August expense, got %+v", openEnded)
	}
}

func TestExpenses_ListPagination(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	category := createExpenseCategory(t, adminToken, "Lain-lain")

	for i := 0; i < 3; i++ {
		createExpenseAPI(t, adminToken, validExpenseBody(category.ID, nil))
	}

	status, resp := doRequest(t, http.MethodGet, "/api/expenses/?limit=2&page=1", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (resp=%+v)", status, resp)
	}
	var page1 expenseListDTO
	decodeData(t, resp, &page1)
	if len(page1.Items) != 2 || page1.Pagination.Total != 3 || page1.Pagination.TotalPages != 2 {
		t.Fatalf("expected 2 items total=3 total_pages=2, got %d items %+v", len(page1.Items), page1.Pagination)
	}
}

func TestExpenses_ListCategoryNotFound(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	status, resp := doRequest(t, http.MethodGet, "/api/expenses/?category_id="+nonexistentUUID, nil, adminToken)
	if status != http.StatusNotFound {
		t.Fatalf("expected 404, got %d (resp=%+v)", status, resp)
	}
}
