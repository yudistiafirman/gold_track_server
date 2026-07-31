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

// --- GET /api/suppliers/{id}/transactions: combined PO + SELL_SUPPLIER history ---

type supplierHistoryEntryDTO struct {
	ID          string  `json:"id"`
	Source      string  `json:"source"`
	Code        string  `json:"code"`
	Status      string  `json:"status"`
	TotalAmount float64 `json:"total_amount"`
}

type supplierHistoryListDTO struct {
	Items      []supplierHistoryEntryDTO `json:"items"`
	Pagination paginationDTO             `json:"pagination"`
}

func TestSuppliers_HistoryRequiresAuth(t *testing.T) {
	resetDB(t)

	status, _ := doRequest(t, http.MethodGet, "/api/suppliers/"+nonexistentUUID+"/transactions", nil, "")
	if status != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", status)
	}
}

func TestSuppliers_HistoryNonAdminForbidden(t *testing.T) {
	resetDB(t)
	kasir := seedUser(t, "KASIR", true)
	token := login(t, kasir.Email, kasir.Password)

	status, resp := doRequest(t, http.MethodGet, "/api/suppliers/"+nonexistentUUID+"/transactions", nil, token)
	if status != http.StatusForbidden {
		t.Fatalf("expected 403, got %d (resp=%+v)", status, resp)
	}
}

func TestSuppliers_HistoryCombinesPOAndSellSupplier(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	supplier := createSupplier(t, adminToken, map[string]any{"name": "Toko Emas Jaya"})

	po := createPurchaseOrder(t, adminToken, supplier.ID, []map[string]any{
		{"product_id": product.ID, "quantity": 2, "purchase_price": 800000},
	})

	stockItem := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(nil))
	status, resp := doRequest(t, http.MethodPost, "/api/transactions", map[string]any{
		"type":           "SELL_SUPPLIER",
		"supplier_id":    supplier.ID,
		"payment_method": "CASH",
		"items":          []map[string]any{{"stock_item_id": stockItem.ID, "price_total": 100000}},
	}, adminToken)
	if status != http.StatusCreated {
		t.Fatalf("sell_supplier: expected 201, got %d (resp=%+v)", status, resp)
	}
	var sellSupplierTx transactionDTO
	decodeData(t, resp, &sellSupplierTx)

	status, resp = doRequest(t, http.MethodGet, "/api/suppliers/"+supplier.ID+"/transactions", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (resp=%+v)", status, resp)
	}
	var history supplierHistoryListDTO
	decodeData(t, resp, &history)

	if history.Pagination.Total != 2 || len(history.Items) != 2 {
		t.Fatalf("expected 2 combined history items, got %+v", history)
	}
	// Newest first — the SELL_SUPPLIER transaction was created after the PO.
	if history.Items[0].ID != sellSupplierTx.ID || history.Items[0].Source != "SELL_SUPPLIER" || history.Items[0].Code != sellSupplierTx.TransactionCode {
		t.Fatalf("expected newest item to be the SELL_SUPPLIER transaction, got %+v", history.Items[0])
	}
	if history.Items[1].ID != po.ID || history.Items[1].Source != "PURCHASE_ORDER" || history.Items[1].Code != po.POCode {
		t.Fatalf("expected oldest item to be the PO, got %+v", history.Items[1])
	}
	if history.Items[1].Status != "BELUM_DITERIMA" || history.Items[1].TotalAmount != 1600000 {
		t.Fatalf("expected PO status/total_amount to match, got %+v", history.Items[1])
	}
}

func TestSuppliers_HistoryExcludesOtherSuppliers(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	supplierA := createSupplier(t, adminToken, map[string]any{"name": "Toko Emas Jaya"})
	supplierB := createSupplier(t, adminToken, map[string]any{"name": "Toko Perak Sentosa"})

	createPurchaseOrder(t, adminToken, supplierB.ID, []map[string]any{
		{"product_id": product.ID, "quantity": 1, "purchase_price": 800000},
	})

	status, resp := doRequest(t, http.MethodGet, "/api/suppliers/"+supplierA.ID+"/transactions", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (resp=%+v)", status, resp)
	}
	var history supplierHistoryListDTO
	decodeData(t, resp, &history)
	if history.Pagination.Total != 0 || len(history.Items) != 0 {
		t.Fatalf("expected empty history for supplier A, got %+v", history)
	}
}

func TestSuppliers_HistoryPagination(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	supplier := createSupplier(t, adminToken, map[string]any{"name": "Toko Emas Jaya"})

	for i := 0; i < 3; i++ {
		createPurchaseOrder(t, adminToken, supplier.ID, []map[string]any{
			{"product_id": product.ID, "quantity": 1, "purchase_price": 800000},
		})
	}

	status, resp := doRequest(t, http.MethodGet, "/api/suppliers/"+supplier.ID+"/transactions?limit=2&page=1", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("page 1: expected 200, got %d (resp=%+v)", status, resp)
	}
	var page1 supplierHistoryListDTO
	decodeData(t, resp, &page1)
	if len(page1.Items) != 2 || page1.Pagination.Total != 3 || page1.Pagination.TotalPages != 2 {
		t.Fatalf("page 1: expected 2 items total=3 total_pages=2, got %d items %+v", len(page1.Items), page1.Pagination)
	}

	status, resp = doRequest(t, http.MethodGet, "/api/suppliers/"+supplier.ID+"/transactions?limit=2&page=2", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("page 2: expected 200, got %d (resp=%+v)", status, resp)
	}
	var page2 supplierHistoryListDTO
	decodeData(t, resp, &page2)
	if len(page2.Items) != 1 {
		t.Fatalf("page 2: expected 1 item, got %d", len(page2.Items))
	}
}

func TestSuppliers_HistoryEmptyIsNotError(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	supplier := createSupplier(t, adminToken, map[string]any{"name": "Toko Emas Jaya"})

	status, resp := doRequest(t, http.MethodGet, "/api/suppliers/"+supplier.ID+"/transactions", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (resp=%+v)", status, resp)
	}
	var history supplierHistoryListDTO
	decodeData(t, resp, &history)
	if history.Pagination.Total != 0 || len(history.Items) != 0 {
		t.Fatalf("expected empty history, not an error, got %+v", history)
	}
}

func TestSuppliers_HistoryNotFound(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	status, resp := doRequest(t, http.MethodGet, "/api/suppliers/"+nonexistentUUID+"/transactions", nil, adminToken)
	if status != http.StatusNotFound {
		t.Fatalf("expected 404, got %d (resp=%+v)", status, resp)
	}
}

func TestSuppliers_HistoryInvalidIDFormat(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	status, resp := doRequest(t, http.MethodGet, "/api/suppliers/1/transactions", nil, adminToken)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (resp=%+v)", status, resp)
	}
}
