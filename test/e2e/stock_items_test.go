package e2e

import (
	"context"
	"net/http"
	"testing"
)

type stockItemDTO struct {
	ID            string        `json:"id"`
	Product       productRefDTO `json:"product"`
	Barcode       string        `json:"barcode"`
	SerialNumber  string        `json:"serial_number"`
	Condition     string        `json:"condition"`
	PurchasePrice float64       `json:"purchase_price"`
	PurchaseDate  string        `json:"purchase_date"`
	Status        string        `json:"status"`
	Notes         string        `json:"notes"`
}

type stockItemListDTO struct {
	Items      []stockItemDTO `json:"items"`
	Pagination paginationDTO  `json:"pagination"`
}

type stockItemLabelDTO struct {
	Barcode      string  `json:"barcode"`
	ProductName  string  `json:"product_name"`
	WeightGram   float64 `json:"weight_gram"`
	SerialNumber string  `json:"serial_number"`
}

// stockItemFixtureProduct creates a category+brand+product via the real
// APIs, returning the product DTO (its SKU drives the expected barcode).
func stockItemFixtureProduct(t *testing.T, adminToken string) productDTO {
	t.Helper()
	category := createCategory(t, adminToken, "Batangan")
	brand := createBrand(t, adminToken, "Antam")
	return createProduct(t, adminToken, "Emas Batangan 10gr", category.ID, brand.ID, 10)
}

func validStockItemBody(overrides map[string]any) map[string]any {
	body := map[string]any{
		"serial_number":  "SN-0001",
		"condition":      "GOOD",
		"purchase_price": 1000000,
		"purchase_date":  "2026-07-01",
	}
	for k, v := range overrides {
		body[k] = v
	}
	return body
}

func createStockItemAPI(t *testing.T, adminToken, productID string, body map[string]any) stockItemDTO {
	t.Helper()
	status, resp := doRequest(t, http.MethodPost, "/api/products/"+productID+"/stock-items", body, adminToken)
	if status != http.StatusCreated {
		t.Fatalf("create stock item fixture: expected 201, got %d (resp=%+v)", status, resp)
	}
	var s stockItemDTO
	decodeData(t, resp, &s)
	return s
}

// markStockItemSold flips status directly via SQL — there's no
// mark-as-sold endpoint yet (belongs to a future sales ticket).
func markStockItemSold(t *testing.T, publicID string) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(), `UPDATE stock_items SET status = 'SOLD' WHERE public_id = $1`, publicID); err != nil {
		t.Fatalf("mark stock item sold: %v", err)
	}
}

const nonexistentUUID = "00000000-0000-0000-0000-000000000000"

// --- BE-501: create ---

func TestStockItems_CreateRequiresAuth(t *testing.T) {
	resetDB(t)

	status, _ := doRequest(t, http.MethodPost, "/api/products/"+nonexistentUUID+"/stock-items", nil, "")
	if status != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", status)
	}
}

func TestStockItems_CreateNonAdminForbidden(t *testing.T) {
	resetDB(t)
	kasir := seedUser(t, "KASIR", true)
	token := login(t, kasir.Email, kasir.Password)

	status, resp := doRequest(t, http.MethodPost, "/api/products/"+nonexistentUUID+"/stock-items", nil, token)
	if status != http.StatusForbidden {
		t.Fatalf("expected 403, got %d (resp=%+v)", status, resp)
	}
}

func TestStockItems_CreateGeneratesBarcode(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)

	status, resp := doRequest(t, http.MethodPost, "/api/products/"+product.ID+"/stock-items", validStockItemBody(nil), adminToken)
	if status != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d (resp=%+v)", status, resp)
	}
	var created stockItemDTO
	decodeData(t, resp, &created)
	expectedBarcode := product.SKU + "-0001"
	if created.Barcode != expectedBarcode {
		t.Fatalf("expected barcode %q, got %q", expectedBarcode, created.Barcode)
	}
	if created.Status != "AVAILABLE" {
		t.Fatalf("expected status AVAILABLE, got %q", created.Status)
	}
	if created.Product.ID != product.ID {
		t.Fatalf("expected product ref to match, got %+v", created.Product)
	}
}

func TestStockItems_BarcodeIncrementsForSameProduct(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)

	first := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(map[string]any{"serial_number": "SN-0001"}))
	second := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(map[string]any{"serial_number": "SN-0002"}))

	if first.Barcode != product.SKU+"-0001" {
		t.Fatalf("expected first barcode %s-0001, got %q", product.SKU, first.Barcode)
	}
	if second.Barcode != product.SKU+"-0002" {
		t.Fatalf("expected second barcode %s-0002, got %q", product.SKU, second.Barcode)
	}
}

func TestStockItems_CreateMissingSerialNumber(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)

	status, resp := doRequest(t, http.MethodPost, "/api/products/"+product.ID+"/stock-items",
		validStockItemBody(map[string]any{"serial_number": ""}), adminToken)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d (resp=%+v)", status, resp)
	}
}

func TestStockItems_CreateInvalidCondition(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)

	for _, condition := range []string{"", "OK"} {
		status, resp := doRequest(t, http.MethodPost, "/api/products/"+product.ID+"/stock-items",
			validStockItemBody(map[string]any{"condition": condition}), adminToken)
		if status != http.StatusUnprocessableEntity {
			t.Fatalf("condition=%q: expected 422, got %d (resp=%+v)", condition, status, resp)
		}
	}
}

func TestStockItems_CreateInvalidPurchasePrice(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)

	status, resp := doRequest(t, http.MethodPost, "/api/products/"+product.ID+"/stock-items",
		validStockItemBody(map[string]any{"purchase_price": 0}), adminToken)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d (resp=%+v)", status, resp)
	}
}

func TestStockItems_CreateMissingPurchaseDate(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)

	for _, date := range []string{"", "07-01-2026"} {
		status, resp := doRequest(t, http.MethodPost, "/api/products/"+product.ID+"/stock-items",
			validStockItemBody(map[string]any{"purchase_date": date}), adminToken)
		if status != http.StatusUnprocessableEntity {
			t.Fatalf("purchase_date=%q: expected 422, got %d (resp=%+v)", date, status, resp)
		}
	}
}

func TestStockItems_CreateProductArchivedRejected(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)

	status, resp := doRequest(t, http.MethodDelete, "/api/products/"+product.ID, nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("archive product: expected 200, got %d (resp=%+v)", status, resp)
	}

	status, resp = doRequest(t, http.MethodPost, "/api/products/"+product.ID+"/stock-items", validStockItemBody(nil), adminToken)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400 for archived product, got %d (resp=%+v)", status, resp)
	}
}

func TestStockItems_CreateProductNotFound(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	status, resp := doRequest(t, http.MethodPost, "/api/products/"+nonexistentUUID+"/stock-items", validStockItemBody(nil), adminToken)
	if status != http.StatusNotFound {
		t.Fatalf("expected 404, got %d (resp=%+v)", status, resp)
	}
}

func TestStockItems_CreateDuplicateSerialNumberRejected(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)

	createStockItemAPI(t, adminToken, product.ID, validStockItemBody(map[string]any{"serial_number": "SN-DUP"}))

	status, resp := doRequest(t, http.MethodPost, "/api/products/"+product.ID+"/stock-items",
		validStockItemBody(map[string]any{"serial_number": "SN-DUP"}), adminToken)
	if status != http.StatusConflict {
		t.Fatalf("expected 409 for duplicate serial_number, got %d (resp=%+v)", status, resp)
	}
}

// --- BE-502: list & detail ---

func TestStockItems_ListRequiresAuth(t *testing.T) {
	resetDB(t)

	status, _ := doRequest(t, http.MethodGet, "/api/products/"+nonexistentUUID+"/stock-items", nil, "")
	if status != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", status)
	}
}

func TestStockItems_ListKasirCanAccess(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	kasir := seedUser(t, "KASIR", true)
	kasirToken := login(t, kasir.Email, kasir.Password)
	product := stockItemFixtureProduct(t, adminToken)
	createStockItemAPI(t, adminToken, product.ID, validStockItemBody(nil))

	status, resp := doRequest(t, http.MethodGet, "/api/products/"+product.ID+"/stock-items", nil, kasirToken)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (resp=%+v)", status, resp)
	}
}

func TestStockItems_ListFiltersStatusAndCondition(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)

	good := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(map[string]any{"serial_number": "SN-GOOD", "condition": "GOOD"}))
	createStockItemAPI(t, adminToken, product.ID, validStockItemBody(map[string]any{"serial_number": "SN-BAD", "condition": "BAD"}))
	sold := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(map[string]any{"serial_number": "SN-SOLD", "condition": "GOOD"}))
	markStockItemSold(t, sold.ID)

	status, resp := doRequest(t, http.MethodGet, "/api/products/"+product.ID+"/stock-items?condition=GOOD", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("condition filter: expected 200, got %d (resp=%+v)", status, resp)
	}
	var byCondition stockItemListDTO
	decodeData(t, resp, &byCondition)
	if len(byCondition.Items) != 2 {
		t.Fatalf("condition=GOOD: expected 2 items, got %d", len(byCondition.Items))
	}

	status, resp = doRequest(t, http.MethodGet, "/api/products/"+product.ID+"/stock-items?status=AVAILABLE", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("status filter: expected 200, got %d (resp=%+v)", status, resp)
	}
	var byStatus stockItemListDTO
	decodeData(t, resp, &byStatus)
	if len(byStatus.Items) != 2 {
		t.Fatalf("status=AVAILABLE: expected 2 items, got %d", len(byStatus.Items))
	}
	for _, item := range byStatus.Items {
		if item.ID == sold.ID {
			t.Fatal("SOLD item must not appear in status=AVAILABLE filter")
		}
	}
	_ = good
}

func TestStockItems_ListSearchBySerial(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)

	createStockItemAPI(t, adminToken, product.ID, validStockItemBody(map[string]any{"serial_number": "ABC-123"}))
	createStockItemAPI(t, adminToken, product.ID, validStockItemBody(map[string]any{"serial_number": "XYZ-999"}))

	status, resp := doRequest(t, http.MethodGet, "/api/products/"+product.ID+"/stock-items?search=ABC", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (resp=%+v)", status, resp)
	}
	var list stockItemListDTO
	decodeData(t, resp, &list)
	if len(list.Items) != 1 || list.Items[0].SerialNumber != "ABC-123" {
		t.Fatalf("search=ABC: expected 1 item ABC-123, got %+v", list.Items)
	}
}

func TestStockItems_ListPagination(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)

	for i := 1; i <= 3; i++ {
		createStockItemAPI(t, adminToken, product.ID, validStockItemBody(map[string]any{"serial_number": product.SKU + "-serial-" + string(rune('0'+i))}))
	}

	status, resp := doRequest(t, http.MethodGet, "/api/products/"+product.ID+"/stock-items?limit=2&page=1", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("page 1: expected 200, got %d (resp=%+v)", status, resp)
	}
	var page1 stockItemListDTO
	decodeData(t, resp, &page1)
	if len(page1.Items) != 2 || page1.Pagination.Total != 3 || page1.Pagination.TotalPages != 2 {
		t.Fatalf("page 1: expected 2 items total=3 total_pages=2, got %d items %+v", len(page1.Items), page1.Pagination)
	}

	status, resp = doRequest(t, http.MethodGet, "/api/products/"+product.ID+"/stock-items?limit=2&page=2", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("page 2: expected 200, got %d (resp=%+v)", status, resp)
	}
	var page2 stockItemListDTO
	decodeData(t, resp, &page2)
	if len(page2.Items) != 1 {
		t.Fatalf("page 2: expected 1 item, got %d", len(page2.Items))
	}
}

func TestStockItems_GetReturnsFullDetailWithBarcode(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	created := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(nil))

	status, resp := doRequest(t, http.MethodGet, "/api/stock-items/"+created.ID, nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (resp=%+v)", status, resp)
	}
	var fetched stockItemDTO
	decodeData(t, resp, &fetched)
	if fetched.Barcode != created.Barcode {
		t.Fatalf("expected barcode %q, got %q", created.Barcode, fetched.Barcode)
	}
	if fetched.SerialNumber != "SN-0001" || fetched.Condition != "GOOD" {
		t.Fatalf("expected full detail, got %+v", fetched)
	}
}

func TestStockItems_GetNotFound(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	status, resp := doRequest(t, http.MethodGet, "/api/stock-items/"+nonexistentUUID, nil, adminToken)
	if status != http.StatusNotFound {
		t.Fatalf("expected 404, got %d (resp=%+v)", status, resp)
	}
}

func TestStockItems_GetInvalidIDFormat(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	status, resp := doRequest(t, http.MethodGet, "/api/stock-items/1", nil, adminToken)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (resp=%+v)", status, resp)
	}
}

// --- BE-503: update ---

func TestStockItems_UpdateRequiresAuth(t *testing.T) {
	resetDB(t)

	status, _ := doRequest(t, http.MethodPut, "/api/stock-items/"+nonexistentUUID, nil, "")
	if status != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", status)
	}
}

func TestStockItems_UpdateNonAdminForbidden(t *testing.T) {
	resetDB(t)
	kasir := seedUser(t, "KASIR", true)
	token := login(t, kasir.Email, kasir.Password)

	status, resp := doRequest(t, http.MethodPut, "/api/stock-items/"+nonexistentUUID, nil, token)
	if status != http.StatusForbidden {
		t.Fatalf("expected 403, got %d (resp=%+v)", status, resp)
	}
}

func TestStockItems_UpdateAppliesChangesButLocksBarcodeAndProduct(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	created := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(nil))

	status, resp := doRequest(t, http.MethodPut, "/api/stock-items/"+created.ID, map[string]any{
		"serial_number":  "SN-CHANGED",
		"condition":      "BAD",
		"purchase_price": 2000000,
		"purchase_date":  "2026-08-01",
		"notes":          "Diperiksa ulang",
	}, adminToken)
	if status != http.StatusOK {
		t.Fatalf("update: expected 200, got %d (resp=%+v)", status, resp)
	}
	var updated stockItemDTO
	decodeData(t, resp, &updated)
	if updated.SerialNumber != "SN-CHANGED" || updated.Condition != "BAD" || updated.PurchasePrice != 2000000 || updated.PurchaseDate != "2026-08-01" || updated.Notes != "Diperiksa ulang" {
		t.Fatalf("update: fields not applied, got %+v", updated)
	}
	if updated.Barcode != created.Barcode {
		t.Fatalf("expected barcode to stay %q, got %q", created.Barcode, updated.Barcode)
	}
	if updated.Product.ID != product.ID {
		t.Fatalf("expected product to stay %q, got %q", product.ID, updated.Product.ID)
	}
}

func TestStockItems_UpdateSoldUnitRejected(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	created := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(nil))
	markStockItemSold(t, created.ID)

	status, resp := doRequest(t, http.MethodPut, "/api/stock-items/"+created.ID, validStockItemBody(nil), adminToken)
	if status != http.StatusConflict {
		t.Fatalf("expected 409, got %d (resp=%+v)", status, resp)
	}
}

func TestStockItems_UpdateNotFound(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	status, resp := doRequest(t, http.MethodPut, "/api/stock-items/"+nonexistentUUID, validStockItemBody(nil), adminToken)
	if status != http.StatusNotFound {
		t.Fatalf("expected 404, got %d (resp=%+v)", status, resp)
	}
}

func TestStockItems_UpdateInvalidFields(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	created := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(nil))

	status, resp := doRequest(t, http.MethodPut, "/api/stock-items/"+created.ID,
		validStockItemBody(map[string]any{"serial_number": ""}), adminToken)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d (resp=%+v)", status, resp)
	}
}

// --- BE-504: delete ---

func TestStockItems_DeleteRequiresAuth(t *testing.T) {
	resetDB(t)

	status, _ := doRequest(t, http.MethodDelete, "/api/stock-items/"+nonexistentUUID, nil, "")
	if status != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", status)
	}
}

func TestStockItems_DeleteNonAdminForbidden(t *testing.T) {
	resetDB(t)
	kasir := seedUser(t, "KASIR", true)
	token := login(t, kasir.Email, kasir.Password)

	status, resp := doRequest(t, http.MethodDelete, "/api/stock-items/"+nonexistentUUID, nil, token)
	if status != http.StatusForbidden {
		t.Fatalf("expected 403, got %d (resp=%+v)", status, resp)
	}
}

func TestStockItems_DeleteAvailableHardDeletes(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	created := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(nil))

	status, resp := doRequest(t, http.MethodDelete, "/api/stock-items/"+created.ID, nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("delete: expected 200, got %d (resp=%+v)", status, resp)
	}

	status, resp = doRequest(t, http.MethodGet, "/api/stock-items/"+created.ID, nil, adminToken)
	if status != http.StatusNotFound {
		t.Fatalf("expected 404 after hard delete (row actually gone), got %d (resp=%+v)", status, resp)
	}
}

func TestStockItems_DeleteSoldUnitRejected(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	created := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(nil))
	markStockItemSold(t, created.ID)

	status, resp := doRequest(t, http.MethodDelete, "/api/stock-items/"+created.ID, nil, adminToken)
	if status != http.StatusConflict {
		t.Fatalf("expected 409, got %d (resp=%+v)", status, resp)
	}

	status, resp = doRequest(t, http.MethodGet, "/api/stock-items/"+created.ID, nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("expected unit to still exist after blocked delete, got %d (resp=%+v)", status, resp)
	}
}

func TestStockItems_DeleteNotFound(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	status, resp := doRequest(t, http.MethodDelete, "/api/stock-items/"+nonexistentUUID, nil, adminToken)
	if status != http.StatusNotFound {
		t.Fatalf("expected 404, got %d (resp=%+v)", status, resp)
	}
}

// --- BE-505: label ---

func TestStockItems_LabelRequiresAuth(t *testing.T) {
	resetDB(t)

	status, _ := doRequest(t, http.MethodGet, "/api/stock-items/"+nonexistentUUID+"/label", nil, "")
	if status != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", status)
	}
}

func TestStockItems_LabelReturnsBarcodeNameWeightSerial(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	created := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(nil))

	status, resp := doRequest(t, http.MethodGet, "/api/stock-items/"+created.ID+"/label", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (resp=%+v)", status, resp)
	}
	var label stockItemLabelDTO
	decodeData(t, resp, &label)
	if label.Barcode != created.Barcode {
		t.Fatalf("expected barcode %q, got %q", created.Barcode, label.Barcode)
	}
	if label.ProductName != product.Name {
		t.Fatalf("expected product_name %q, got %q", product.Name, label.ProductName)
	}
	if label.WeightGram != 10 {
		t.Fatalf("expected weight_gram=10, got %v", label.WeightGram)
	}
	if label.SerialNumber != "SN-0001" {
		t.Fatalf("expected serial_number SN-0001, got %q", label.SerialNumber)
	}
}

func TestStockItems_LabelWorksForSoldUnit(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	created := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(nil))
	markStockItemSold(t, created.ID)

	status, resp := doRequest(t, http.MethodGet, "/api/stock-items/"+created.ID+"/label", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("expected 200 for SOLD unit label (reprint allowed), got %d (resp=%+v)", status, resp)
	}
}

func TestStockItems_LabelNotFound(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	status, resp := doRequest(t, http.MethodGet, "/api/stock-items/"+nonexistentUUID+"/label", nil, adminToken)
	if status != http.StatusNotFound {
		t.Fatalf("expected 404, got %d (resp=%+v)", status, resp)
	}
}

// --- BE-701/BE-703: barcode lookup ---

type stockItemLookupDTO struct {
	stockItemDTO
	RequiresConfirmation bool `json:"requires_confirmation"`
}

func TestStockItems_LookupRequiresAuth(t *testing.T) {
	resetDB(t)

	status, _ := doRequest(t, http.MethodGet, "/api/stock-items/lookup?barcode=whatever", nil, "")
	if status != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", status)
	}
}

func TestStockItems_LookupFoundAvailable(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	created := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(nil))

	status, resp := doRequest(t, http.MethodGet, "/api/stock-items/lookup?barcode="+created.Barcode, nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (resp=%+v)", status, resp)
	}
	var found stockItemLookupDTO
	decodeData(t, resp, &found)
	if found.Barcode != created.Barcode || found.Condition != "GOOD" || found.Product.ID != product.ID {
		t.Fatalf("expected unit+product+condition, got %+v", found)
	}
}

func TestStockItems_LookupNotFound(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	status, resp := doRequest(t, http.MethodGet, "/api/stock-items/lookup?barcode=NOPE-0000", nil, adminToken)
	if status != http.StatusNotFound {
		t.Fatalf("expected 404, got %d (resp=%+v)", status, resp)
	}
}

func TestStockItems_LookupSoldConflict(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	created := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(nil))
	markStockItemSold(t, created.ID)

	status, resp := doRequest(t, http.MethodGet, "/api/stock-items/lookup?barcode="+created.Barcode, nil, adminToken)
	if status != http.StatusConflict {
		t.Fatalf("expected 409, got %d (resp=%+v)", status, resp)
	}
}

func TestStockItems_LookupRequiresConfirmationForBadConditionSell(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	created := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(map[string]any{"condition": "BAD"}))

	status, resp := doRequest(t, http.MethodGet, "/api/stock-items/lookup?barcode="+created.Barcode+"&type=SELL", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (resp=%+v)", status, resp)
	}
	var found stockItemLookupDTO
	decodeData(t, resp, &found)
	if !found.RequiresConfirmation {
		t.Fatal("expected requires_confirmation=true for BAD condition + type=SELL")
	}
}

func TestStockItems_LookupNoConfirmationForSupplierType(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	created := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(map[string]any{"condition": "BAD"}))

	status, resp := doRequest(t, http.MethodGet, "/api/stock-items/lookup?barcode="+created.Barcode+"&type=SELL_SUPPLIER", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (resp=%+v)", status, resp)
	}
	var found stockItemLookupDTO
	decodeData(t, resp, &found)
	if found.RequiresConfirmation {
		t.Fatal("expected requires_confirmation=false for SELL_SUPPLIER")
	}
}

func TestStockItems_LookupNoConfirmationWhenTypeOmitted(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	created := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(map[string]any{"condition": "BAD"}))

	status, resp := doRequest(t, http.MethodGet, "/api/stock-items/lookup?barcode="+created.Barcode, nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (resp=%+v)", status, resp)
	}
	var found stockItemLookupDTO
	decodeData(t, resp, &found)
	if found.RequiresConfirmation {
		t.Fatal("expected requires_confirmation=false when type is omitted")
	}
}
