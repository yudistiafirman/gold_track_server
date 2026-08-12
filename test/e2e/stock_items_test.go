package e2e

import (
	"context"
	"net/http"
	"regexp"
	"testing"
	"time"
)

type stockItemProductDTO struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	WeightGram float64 `json:"weight_gram"`
}

type stockItemDTO struct {
	ID             string              `json:"id"`
	Product        stockItemProductDTO `json:"product"`
	Barcode        string              `json:"barcode"`
	SerialNumber   string              `json:"serial_number"`
	Condition      string              `json:"condition"`
	PurchasePrice  float64             `json:"purchase_price"`
	PurchaseDate   string              `json:"purchase_date"`
	ProductionYear *int                `json:"production_year"`
	Status         string              `json:"status"`
	SoldAt         *time.Time          `json:"sold_at"`
	Notes          string              `json:"notes"`
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

// markStockItemSold flips status directly via SQL (mirroring what a real
// sale sets: status=SOLD plus sold_at) — there's no mark-as-sold endpoint
// standing alone from a full checkout flow.
func markStockItemSold(t *testing.T, publicID string) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(), `UPDATE stock_items SET status = 'SOLD', sold_at = now() WHERE public_id = $1`, publicID); err != nil {
		t.Fatalf("mark stock item sold: %v", err)
	}
}

const nonexistentUUID = "00000000-0000-0000-0000-000000000000"

// barcodePattern matches the short, scanner-friendly generated barcode
// format (8-digit zero-padded value of stock_items_barcode_seq) — no
// longer SKU-prefixed (that made physical labels unreadable), so tests
// assert shape/uniqueness/ordering instead of a literal expected value.
var barcodePattern = regexp.MustCompile(`^\d{8}$`)

func assertValidGeneratedBarcode(t *testing.T, barcode string) {
	t.Helper()
	if !barcodePattern.MatchString(barcode) {
		t.Fatalf("expected an 8-digit generated barcode, got %q", barcode)
	}
}

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
	assertValidGeneratedBarcode(t, created.Barcode)
	if created.Status != "AVAILABLE" {
		t.Fatalf("expected status AVAILABLE, got %q", created.Status)
	}
	if created.Product.ID != product.ID {
		t.Fatalf("expected product ref to match, got %+v", created.Product)
	}
}

func TestStockItems_CreateWithProductionYear(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)

	withYear := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(map[string]any{
		"serial_number":   "SN-YEAR-1",
		"production_year": 2024,
	}))
	if withYear.ProductionYear == nil || *withYear.ProductionYear != 2024 {
		t.Fatalf("expected production_year 2024, got %v", withYear.ProductionYear)
	}

	withoutYear := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(map[string]any{"serial_number": "SN-YEAR-2"}))
	if withoutYear.ProductionYear != nil {
		t.Fatalf("expected production_year nil when omitted, got %v", withoutYear.ProductionYear)
	}
}

func TestStockItems_CreateInvalidProductionYear(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)

	status, resp := doRequest(t, http.MethodPost, "/api/products/"+product.ID+"/stock-items",
		validStockItemBody(map[string]any{"production_year": 1899}), adminToken)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d (resp=%+v)", status, resp)
	}
}

// TestStockItems_BarcodeIsGlobalSequenceNotPerProduct covers the barcode
// generator's move off SKU-prefixed-per-product counting (too long to
// render on a 50x25mm physical label) to a bare global sequence: two units
// of the *same* product get distinct, monotonically increasing barcodes
// with no shared prefix.
func TestStockItems_BarcodeIsGlobalSequenceNotPerProduct(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)

	first := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(map[string]any{"serial_number": "SN-0001"}))
	second := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(map[string]any{"serial_number": "SN-0002"}))

	assertValidGeneratedBarcode(t, first.Barcode)
	assertValidGeneratedBarcode(t, second.Barcode)
	if first.Barcode == second.Barcode {
		t.Fatalf("expected distinct barcodes, both were %q", first.Barcode)
	}
	if second.Barcode <= first.Barcode {
		t.Fatalf("expected second barcode > first (monotonic sequence), got first=%q second=%q", first.Barcode, second.Barcode)
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

// TestStockItems_ListHidesDeadStatusesByDefaultButShowsOnExplicitFilter covers
// both "dead" statuses a unit can end up in outside the normal AVAILABLE/SOLD
// lifecycle: ARCHIVED (admin removed it) and VOID (its originating BUY was
// cancelled). Neither is real usable stock, so both are hidden from the
// default (no ?status=) list — matching how every other resource hides its
// inactive rows by default — but each stays reachable via an explicit
// ?status= filter for audit purposes.
func TestStockItems_ListHidesDeadStatusesByDefaultButShowsOnExplicitFilter(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	superAdmin := seedUser(t, "SUPER_ADMIN", true)
	superAdminToken := login(t, superAdmin.Email, superAdmin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	customer := createCustomer(t, adminToken, map[string]any{"name": "Budi Santoso"})

	kept := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(map[string]any{"serial_number": "SN-KEEP"}))

	archived := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(map[string]any{"serial_number": "SN-ARC"}))
	status, resp := doRequest(t, http.MethodDelete, "/api/stock-items/"+archived.ID, nil, superAdminToken)
	if status != http.StatusOK {
		t.Fatalf("archive: expected 200, got %d (resp=%+v)", status, resp)
	}

	status, resp = doRequest(t, http.MethodPost, "/api/transactions", buyTransactionBody(customer.ID, []map[string]any{
		{"product_id": product.ID, "serial_number": "SN-VOID", "condition": "GOOD", "price_total": 900000},
	}), adminToken)
	if status != http.StatusCreated {
		t.Fatalf("buy: expected 201, got %d (resp=%+v)", status, resp)
	}
	var buyTx transactionDTO
	decodeData(t, resp, &buyTx)
	voidedID := buyTx.Items[0].StockItemID
	status, resp = doRequest(t, http.MethodPost, "/api/transactions/"+buyTx.ID+"/cancel", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("cancel buy: expected 200, got %d (resp=%+v)", status, resp)
	}

	status, resp = doRequest(t, http.MethodGet, "/api/products/"+product.ID+"/stock-items", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("list: expected 200, got %d (resp=%+v)", status, resp)
	}
	var unfiltered stockItemListDTO
	decodeData(t, resp, &unfiltered)
	if len(unfiltered.Items) != 1 || unfiltered.Items[0].ID != kept.ID {
		t.Fatalf("expected default list to hide ARCHIVED and VOID units and only show %q, got %+v", kept.ID, unfiltered.Items)
	}

	status, resp = doRequest(t, http.MethodGet, "/api/products/"+product.ID+"/stock-items?status=ARCHIVED", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("list status=ARCHIVED: expected 200, got %d (resp=%+v)", status, resp)
	}
	var byArchived stockItemListDTO
	decodeData(t, resp, &byArchived)
	if len(byArchived.Items) != 1 || byArchived.Items[0].ID != archived.ID {
		t.Fatalf("expected explicit status=ARCHIVED filter to return %q, got %+v", archived.ID, byArchived.Items)
	}

	status, resp = doRequest(t, http.MethodGet, "/api/products/"+product.ID+"/stock-items?status=VOID", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("list status=VOID: expected 200, got %d (resp=%+v)", status, resp)
	}
	var byVoid stockItemListDTO
	decodeData(t, resp, &byVoid)
	if len(byVoid.Items) != 1 || byVoid.Items[0].ID != voidedID {
		t.Fatalf("expected explicit status=VOID filter to return %q, got %+v", voidedID, byVoid.Items)
	}
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

func TestStockItems_UpdateArchivedUnitRejected(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	superAdmin := seedUser(t, "SUPER_ADMIN", true)
	superAdminToken := login(t, superAdmin.Email, superAdmin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	created := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(nil))

	status, resp := doRequest(t, http.MethodDelete, "/api/stock-items/"+created.ID, nil, superAdminToken)
	if status != http.StatusOK {
		t.Fatalf("archive: expected 200, got %d (resp=%+v)", status, resp)
	}

	status, resp = doRequest(t, http.MethodPut, "/api/stock-items/"+created.ID, validStockItemBody(nil), adminToken)
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

// TestStockItems_DeleteNonSuperAdminForbidden covers the client requirement
// that archiving a stock item is SUPER_ADMIN-only — plain ADMIN keeps
// Create/Update on stock items but not Delete, same tier as KASIR here.
func TestStockItems_DeleteNonSuperAdminForbidden(t *testing.T) {
	resetDB(t)
	kasir := seedUser(t, "KASIR", true)
	kasirToken := login(t, kasir.Email, kasir.Password)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	for _, tc := range []struct {
		role  string
		token string
	}{{"KASIR", kasirToken}, {"ADMIN", adminToken}} {
		status, resp := doRequest(t, http.MethodDelete, "/api/stock-items/"+nonexistentUUID, nil, tc.token)
		if status != http.StatusForbidden {
			t.Fatalf("delete as %s: expected 403, got %d (resp=%+v)", tc.role, status, resp)
		}
	}
}

func TestStockItems_DeleteAvailableArchives(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	superAdmin := seedUser(t, "SUPER_ADMIN", true)
	superAdminToken := login(t, superAdmin.Email, superAdmin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	created := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(nil))

	status, resp := doRequest(t, http.MethodDelete, "/api/stock-items/"+created.ID, nil, superAdminToken)
	if status != http.StatusOK {
		t.Fatalf("delete: expected 200, got %d (resp=%+v)", status, resp)
	}

	status, resp = doRequest(t, http.MethodGet, "/api/stock-items/"+created.ID, nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("expected 200 after archive (row kept, not hard-deleted), got %d (resp=%+v)", status, resp)
	}
	var fetched stockItemDTO
	decodeData(t, resp, &fetched)
	if fetched.Status != "ARCHIVED" {
		t.Fatalf("expected status ARCHIVED after delete, got %q", fetched.Status)
	}
}

// TestStockItems_DeleteSoldUnitArchives covers archiving a unit that's
// already SOLD — allowed (unlike VOID), since a completed sale is
// legitimate history the shop may still want out of the default list.
// sold_at must survive the archive untouched, since it's the only record of
// when the unit sold.
func TestStockItems_DeleteSoldUnitArchives(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	superAdmin := seedUser(t, "SUPER_ADMIN", true)
	superAdminToken := login(t, superAdmin.Email, superAdmin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	created := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(nil))
	markStockItemSold(t, created.ID)

	status, resp := doRequest(t, http.MethodGet, "/api/stock-items/"+created.ID, nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("get before archive: expected 200, got %d (resp=%+v)", status, resp)
	}
	var beforeArchive stockItemDTO
	decodeData(t, resp, &beforeArchive)

	status, resp = doRequest(t, http.MethodDelete, "/api/stock-items/"+created.ID, nil, superAdminToken)
	if status != http.StatusOK {
		t.Fatalf("archive: expected 200, got %d (resp=%+v)", status, resp)
	}

	status, resp = doRequest(t, http.MethodGet, "/api/stock-items/"+created.ID, nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("get after archive: expected 200, got %d (resp=%+v)", status, resp)
	}
	var afterArchive stockItemDTO
	decodeData(t, resp, &afterArchive)
	if afterArchive.Status != "ARCHIVED" {
		t.Fatalf("expected status ARCHIVED, got %q", afterArchive.Status)
	}
	if afterArchive.SoldAt == nil || beforeArchive.SoldAt == nil || !afterArchive.SoldAt.Equal(*beforeArchive.SoldAt) {
		t.Fatalf("expected sold_at to survive archiving unchanged, before=%v after=%v", beforeArchive.SoldAt, afterArchive.SoldAt)
	}
}

func TestStockItems_DeleteAlreadyArchivedRejected(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	superAdmin := seedUser(t, "SUPER_ADMIN", true)
	superAdminToken := login(t, superAdmin.Email, superAdmin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	created := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(nil))

	status, resp := doRequest(t, http.MethodDelete, "/api/stock-items/"+created.ID, nil, superAdminToken)
	if status != http.StatusOK {
		t.Fatalf("first archive: expected 200, got %d (resp=%+v)", status, resp)
	}

	status, resp = doRequest(t, http.MethodDelete, "/api/stock-items/"+created.ID, nil, superAdminToken)
	if status != http.StatusConflict {
		t.Fatalf("expected 409 archiving an already-archived unit, got %d (resp=%+v)", status, resp)
	}
}

// TestStockItems_DeleteVoidUnitRejected covers archiving a unit whose
// originating BUY transaction was cancelled (status VOID) — VOID stays a
// distinct terminal status (it records *why* the unit isn't real stock),
// it's never allowed to transition into ARCHIVED.
func TestStockItems_DeleteVoidUnitRejected(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	superAdmin := seedUser(t, "SUPER_ADMIN", true)
	superAdminToken := login(t, superAdmin.Email, superAdmin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	customer := createCustomer(t, adminToken, map[string]any{"name": "Budi Santoso"})

	status, resp := doRequest(t, http.MethodPost, "/api/transactions", buyTransactionBody(customer.ID, []map[string]any{
		{"product_id": product.ID, "serial_number": "SN-VOID-DEL", "condition": "GOOD", "price_total": 900000},
	}), adminToken)
	if status != http.StatusCreated {
		t.Fatalf("buy: expected 201, got %d (resp=%+v)", status, resp)
	}
	var buyTx transactionDTO
	decodeData(t, resp, &buyTx)
	stockItemID := buyTx.Items[0].StockItemID
	status, resp = doRequest(t, http.MethodPost, "/api/transactions/"+buyTx.ID+"/cancel", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("cancel buy: expected 200, got %d (resp=%+v)", status, resp)
	}

	status, resp = doRequest(t, http.MethodDelete, "/api/stock-items/"+stockItemID, nil, superAdminToken)
	if status != http.StatusConflict {
		t.Fatalf("expected 409 archiving a VOID unit, got %d (resp=%+v)", status, resp)
	}
}

// TestStockItems_DeleteReferencedByBuyTransactionSucceeds covers a unit
// that stays AVAILABLE right after creation but is already referenced by
// transaction_items (a BUY/buyback line item) — this used to be rejected
// with a 409 because a hard delete would have violated the FK, but
// archiving (an UPDATE, not a DELETE) never touches that row, so it must
// now succeed cleanly.
func TestStockItems_DeleteReferencedByBuyTransactionSucceeds(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	superAdmin := seedUser(t, "SUPER_ADMIN", true)
	superAdminToken := login(t, superAdmin.Email, superAdmin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	customer := createCustomer(t, adminToken, map[string]any{"name": "Budi Santoso"})

	status, resp := doRequest(t, http.MethodPost, "/api/transactions", buyTransactionBody(customer.ID, []map[string]any{
		{"product_id": product.ID, "serial_number": "SN-DEL-BUY", "condition": "GOOD", "price_total": 900000},
	}), adminToken)
	if status != http.StatusCreated {
		t.Fatalf("buy: expected 201, got %d (resp=%+v)", status, resp)
	}
	var tx transactionDTO
	decodeData(t, resp, &tx)
	stockItemID := tx.Items[0].StockItemID

	status, resp = doRequest(t, http.MethodDelete, "/api/stock-items/"+stockItemID, nil, superAdminToken)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (resp=%+v)", status, resp)
	}

	status, resp = doRequest(t, http.MethodGet, "/api/stock-items/"+stockItemID, nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("expected unit to still exist after archive, got %d (resp=%+v)", status, resp)
	}
	var fetched stockItemDTO
	decodeData(t, resp, &fetched)
	if fetched.Status != "ARCHIVED" {
		t.Fatalf("expected status ARCHIVED, got %q", fetched.Status)
	}
}

// TestStockItems_DeleteReferencedByStockOpnameSucceeds covers a unit that
// was scanned during a stock opname session (stock_opname_items row
// created) — same FK-referenced case as the BUY test above, now succeeds
// for the same reason (archive is an UPDATE, never violates the FK).
func TestStockItems_DeleteReferencedByStockOpnameSucceeds(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	superAdmin := seedUser(t, "SUPER_ADMIN", true)
	superAdminToken := login(t, superAdmin.Email, superAdmin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	created := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(nil))

	opname := createStockOpname(t, adminToken)
	status, resp := doRequest(t, http.MethodPost, "/api/stock-opnames/"+opname.ID+"/scan", map[string]any{"barcode": created.Barcode}, adminToken)
	if status != http.StatusOK {
		t.Fatalf("scan: expected 200, got %d (resp=%+v)", status, resp)
	}

	status, resp = doRequest(t, http.MethodDelete, "/api/stock-items/"+created.ID, nil, superAdminToken)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (resp=%+v)", status, resp)
	}
}

func TestStockItems_DeleteNotFound(t *testing.T) {
	resetDB(t)
	superAdmin := seedUser(t, "SUPER_ADMIN", true)
	superAdminToken := login(t, superAdmin.Email, superAdmin.Password)

	status, resp := doRequest(t, http.MethodDelete, "/api/stock-items/"+nonexistentUUID, nil, superAdminToken)
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
	if found.Product.WeightGram != 10 {
		t.Fatalf("expected product.weight_gram 10, got %v", found.Product.WeightGram)
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

func TestStockItems_LookupArchivedConflict(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	superAdmin := seedUser(t, "SUPER_ADMIN", true)
	superAdminToken := login(t, superAdmin.Email, superAdmin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	created := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(nil))

	status, resp := doRequest(t, http.MethodDelete, "/api/stock-items/"+created.ID, nil, superAdminToken)
	if status != http.StatusOK {
		t.Fatalf("archive: expected 200, got %d (resp=%+v)", status, resp)
	}

	status, resp = doRequest(t, http.MethodGet, "/api/stock-items/lookup?barcode="+created.Barcode, nil, adminToken)
	if status != http.StatusConflict {
		t.Fatalf("expected 409 looking up an archived unit, got %d (resp=%+v)", status, resp)
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
