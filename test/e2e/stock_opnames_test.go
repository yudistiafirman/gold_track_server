package e2e

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

type stockOpnameItemDTO struct {
	ID             string `json:"id"`
	StockItemID    string `json:"stock_item_id"`
	Barcode        string `json:"barcode"`
	ProductName    string `json:"product_name"`
	SystemStatus   string `json:"system_status"`
	PhysicalStatus string `json:"physical_status"`
	Result         string `json:"result"`
}

// scanStockOpnameItemDTO decodes a POST .../scan response — the scanned
// item's fields plus the running not_scanned count/detail.
type scanStockOpnameItemDTO struct {
	stockOpnameItemDTO
	NotScanned         int                            `json:"not_scanned"`
	NotScannedItems    []stockOpnameNotScannedItemDTO `json:"not_scanned_items"`
	NotScannedByWeight []stockOpnameWeightGroupDTO    `json:"not_scanned_by_weight"`
}

type stockOpnameSummaryCountsDTO struct {
	Match      int `json:"match"`
	Missing    int `json:"missing"`
	Unexpected int `json:"unexpected"`
	NotScanned int `json:"not_scanned"`
}

// stockOpnameNotScannedItemDTO is an AVAILABLE stock item still unscanned
// in an IN_PROGRESS session.
type stockOpnameNotScannedItemDTO struct {
	StockItemID string  `json:"stock_item_id"`
	Barcode     string  `json:"barcode"`
	SKU         string  `json:"sku"`
	ProductName string  `json:"product_name"`
	WeightGram  float64 `json:"weight_gram"`
}

// stockOpnameWeightGroupDTO is not-yet-scanned units grouped by weight_gram.
type stockOpnameWeightGroupDTO struct {
	WeightGram      float64 `json:"weight_gram"`
	Count           int     `json:"count"`
	TotalWeightGram float64 `json:"total_weight_gram"`
}

type stockOpnameDTO struct {
	ID                 string                         `json:"id"`
	OpnameCode         string                         `json:"opname_code"`
	OpnameDate         string                         `json:"opname_date"`
	Status             string                         `json:"status"`
	Notes              string                         `json:"notes"`
	Items              []stockOpnameItemDTO           `json:"items"`
	Summary            stockOpnameSummaryCountsDTO    `json:"summary"`
	NotScannedItems    []stockOpnameNotScannedItemDTO `json:"not_scanned_items"`
	NotScannedByWeight []stockOpnameWeightGroupDTO    `json:"not_scanned_by_weight"`
}

func createStockOpname(t *testing.T, adminToken string) stockOpnameDTO {
	t.Helper()
	status, resp := doRequest(t, http.MethodPost, "/api/stock-opnames/", map[string]any{}, adminToken)
	if status != http.StatusCreated {
		t.Fatalf("create stock opname fixture: expected 201, got %d (resp=%+v)", status, resp)
	}
	var o stockOpnameDTO
	decodeData(t, resp, &o)
	return o
}

// --- BE-1101: create session ---

func TestStockOpnames_CreateRequiresAuth(t *testing.T) {
	resetDB(t)

	status, _ := doRequest(t, http.MethodPost, "/api/stock-opnames/", nil, "")
	if status != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", status)
	}
}

func TestStockOpnames_CreateNonAdminForbidden(t *testing.T) {
	resetDB(t)
	kasir := seedUser(t, "KASIR", true)
	token := login(t, kasir.Email, kasir.Password)

	status, resp := doRequest(t, http.MethodPost, "/api/stock-opnames/", map[string]any{}, token)
	if status != http.StatusForbidden {
		t.Fatalf("expected 403, got %d (resp=%+v)", status, resp)
	}
}

func TestStockOpnames_CreateSuccess(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	status, resp := doRequest(t, http.MethodPost, "/api/stock-opnames/", map[string]any{"notes": "opname bulanan"}, adminToken)
	if status != http.StatusCreated {
		t.Fatalf("expected 201, got %d (resp=%+v)", status, resp)
	}
	var o stockOpnameDTO
	decodeData(t, resp, &o)

	today := time.Now().Format("20060102")
	expectedCode := "OPN-" + today + "-0001"
	if o.OpnameCode != expectedCode {
		t.Fatalf("expected opname_code %q, got %q", expectedCode, o.OpnameCode)
	}
	if o.Status != "IN_PROGRESS" {
		t.Fatalf("expected status IN_PROGRESS, got %q", o.Status)
	}
	if o.Notes != "opname bulanan" {
		t.Fatalf("expected notes preserved, got %q", o.Notes)
	}
	if len(o.Items) != 0 {
		t.Fatalf("expected no items right after create, got %+v", o.Items)
	}
}

func TestStockOpnames_CreateCodeIncrementsSameDay(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	first := createStockOpname(t, adminToken)
	// Only one IN_PROGRESS session is allowed at a time — complete the
	// first before creating the second to isolate this test to what it's
	// actually verifying (opname_code increments same-day).
	if status, resp := doRequest(t, http.MethodPost, "/api/stock-opnames/"+first.ID+"/complete", nil, adminToken); status != http.StatusOK {
		t.Fatalf("complete first: expected 200, got %d (resp=%+v)", status, resp)
	}
	second := createStockOpname(t, adminToken)

	today := time.Now().Format("20060102")
	if first.OpnameCode != "OPN-"+today+"-0001" {
		t.Fatalf("expected first code -0001, got %q", first.OpnameCode)
	}
	if second.OpnameCode != "OPN-"+today+"-0002" {
		t.Fatalf("expected second code -0002, got %q", second.OpnameCode)
	}
}

func TestStockOpnames_CreateWhileInProgressRejected(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	createStockOpname(t, adminToken)

	status, resp := doRequest(t, http.MethodPost, "/api/stock-opnames/", map[string]any{}, adminToken)
	if status != http.StatusConflict {
		t.Fatalf("expected 409 while a session is already IN_PROGRESS, got %d (resp=%+v)", status, resp)
	}
}

// --- list ---

type stockOpnameListDTO struct {
	Items      []stockOpnameDTO `json:"items"`
	Pagination paginationDTO    `json:"pagination"`
}

func TestStockOpnames_ListRequiresAuth(t *testing.T) {
	resetDB(t)

	status, _ := doRequest(t, http.MethodGet, "/api/stock-opnames/", nil, "")
	if status != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", status)
	}
}

func TestStockOpnames_ListNonAdminForbidden(t *testing.T) {
	resetDB(t)
	kasir := seedUser(t, "KASIR", true)
	token := login(t, kasir.Email, kasir.Password)

	status, resp := doRequest(t, http.MethodGet, "/api/stock-opnames/", nil, token)
	if status != http.StatusForbidden {
		t.Fatalf("expected 403, got %d (resp=%+v)", status, resp)
	}
}

// TestStockOpnames_ListIncludesInProgressSessions is the whole point of
// having a list endpoint at all — an IN_PROGRESS session must be
// discoverable so it can be resumed (via Scan) later, not just sessions
// that already reached COMPLETED.
func TestStockOpnames_ListIncludesInProgressSessions(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	inProgress := createStockOpname(t, adminToken)

	status, resp := doRequest(t, http.MethodGet, "/api/stock-opnames/", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (resp=%+v)", status, resp)
	}
	var list stockOpnameListDTO
	decodeData(t, resp, &list)

	if len(list.Items) != 1 || list.Items[0].ID != inProgress.ID {
		t.Fatalf("expected the IN_PROGRESS session in the list, got %+v", list.Items)
	}
	if list.Items[0].Status != "IN_PROGRESS" {
		t.Fatalf("expected status IN_PROGRESS, got %q", list.Items[0].Status)
	}
	if len(list.Items[0].Items) != 0 {
		t.Fatalf("expected list rows to be header-only (no items[]), got %+v", list.Items[0].Items)
	}
}

func TestStockOpnames_ListFiltersByStatusAndOrdersNewestFirst(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	completed := createStockOpname(t, adminToken)
	if status, resp := doRequest(t, http.MethodPost, "/api/stock-opnames/"+completed.ID+"/complete", nil, adminToken); status != http.StatusOK {
		t.Fatalf("complete: expected 200, got %d (resp=%+v)", status, resp)
	}
	inProgress := createStockOpname(t, adminToken)

	// No filter: both sessions, newest (still-open) first.
	status, resp := doRequest(t, http.MethodGet, "/api/stock-opnames/", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (resp=%+v)", status, resp)
	}
	var all stockOpnameListDTO
	decodeData(t, resp, &all)
	if len(all.Items) != 2 {
		t.Fatalf("expected 2 sessions total, got %d", len(all.Items))
	}
	if all.Items[0].ID != inProgress.ID || all.Items[1].ID != completed.ID {
		t.Fatalf("expected newest-first order [in_progress, completed], got %+v", all.Items)
	}

	// Filtered: only IN_PROGRESS.
	status, resp = doRequest(t, http.MethodGet, "/api/stock-opnames/?status=IN_PROGRESS", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (resp=%+v)", status, resp)
	}
	var filtered stockOpnameListDTO
	decodeData(t, resp, &filtered)
	if len(filtered.Items) != 1 || filtered.Items[0].ID != inProgress.ID {
		t.Fatalf("expected only the IN_PROGRESS session, got %+v", filtered.Items)
	}
}

func TestStockOpnames_ListInvalidStatus(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	status, resp := doRequest(t, http.MethodGet, "/api/stock-opnames/?status=BOGUS", nil, adminToken)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (resp=%+v)", status, resp)
	}
}

// --- BE-1102: scan ---

func TestStockOpnames_ScanRequiresAuth(t *testing.T) {
	resetDB(t)

	status, _ := doRequest(t, http.MethodPost, "/api/stock-opnames/"+nonexistentUUID+"/scan", map[string]any{"barcode": "X"}, "")
	if status != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", status)
	}
}

func TestStockOpnames_ScanNonAdminForbidden(t *testing.T) {
	resetDB(t)
	kasir := seedUser(t, "KASIR", true)
	token := login(t, kasir.Email, kasir.Password)

	status, resp := doRequest(t, http.MethodPost, "/api/stock-opnames/"+nonexistentUUID+"/scan", map[string]any{"barcode": "X"}, token)
	if status != http.StatusForbidden {
		t.Fatalf("expected 403, got %d (resp=%+v)", status, resp)
	}
}

func TestStockOpnames_ScanAvailableUnitMatches(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	stockItem := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(nil))
	opname := createStockOpname(t, adminToken)

	status, resp := doRequest(t, http.MethodPost, "/api/stock-opnames/"+opname.ID+"/scan", map[string]any{"barcode": stockItem.Barcode}, adminToken)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (resp=%+v)", status, resp)
	}
	var item stockOpnameItemDTO
	decodeData(t, resp, &item)

	if item.Result != "MATCH" {
		t.Fatalf("expected result MATCH, got %q", item.Result)
	}
	if item.SystemStatus != "AVAILABLE" || item.PhysicalStatus != "FOUND" {
		t.Fatalf("expected system_status=AVAILABLE physical_status=FOUND, got %+v", item)
	}
	if item.StockItemID != stockItem.ID || item.Barcode != stockItem.Barcode {
		t.Fatalf("expected stock item identity to match, got %+v", item)
	}
}

// TestStockOpnames_NotScannedCountVisibleBeforeComplete guards the client
// complaint this feature fixes: previously the only way to see which
// AVAILABLE units were still unscanned was to Complete the session (which
// materializes them as MISSING) — by then it's too late to keep scanning
// without starting a whole new session. not_scanned must be visible on both
// GET and the live Scan response while the session is still IN_PROGRESS.
func TestStockOpnames_NotScannedCountVisibleBeforeComplete(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	scanned := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(map[string]any{"serial_number": "NOTSCANNED-SN-1"}))
	_ = createStockItemAPI(t, adminToken, product.ID, validStockItemBody(map[string]any{"serial_number": "NOTSCANNED-SN-2"}))
	_ = createStockItemAPI(t, adminToken, product.ID, validStockItemBody(map[string]any{"serial_number": "NOTSCANNED-SN-3"}))

	opname := createStockOpname(t, adminToken)

	// Before any scan: all 3 AVAILABLE units are still unscanned.
	status, resp := doRequest(t, http.MethodGet, "/api/stock-opnames/"+opname.ID, nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("get: expected 200, got %d (resp=%+v)", status, resp)
	}
	var fetched stockOpnameDTO
	decodeData(t, resp, &fetched)
	if fetched.Summary.NotScanned != 3 {
		t.Fatalf("expected not_scanned=3 before any scan, got %+v", fetched.Summary)
	}

	// The scan response itself reflects the drop immediately, without a
	// separate GET.
	status, resp = doRequest(t, http.MethodPost, "/api/stock-opnames/"+opname.ID+"/scan", map[string]any{"barcode": scanned.Barcode}, adminToken)
	if status != http.StatusOK {
		t.Fatalf("scan: expected 200, got %d (resp=%+v)", status, resp)
	}
	var scanResult scanStockOpnameItemDTO
	decodeData(t, resp, &scanResult)
	if scanResult.NotScanned != 2 {
		t.Fatalf("expected not_scanned=2 right after scanning 1 of 3, got %+v", scanResult)
	}

	// GET agrees.
	status, resp = doRequest(t, http.MethodGet, "/api/stock-opnames/"+opname.ID, nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("get: expected 200, got %d (resp=%+v)", status, resp)
	}
	decodeData(t, resp, &fetched)
	if fetched.Summary.NotScanned != 2 {
		t.Fatalf("expected not_scanned=2 on GET after 1 scan, got %+v", fetched.Summary)
	}

	// Once completed, not_scanned drops to 0 — those units are now MISSING
	// items instead of a pending count.
	status, resp = doRequest(t, http.MethodPost, "/api/stock-opnames/"+opname.ID+"/complete", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("complete: expected 200, got %d (resp=%+v)", status, resp)
	}
	var completed stockOpnameDTO
	decodeData(t, resp, &completed)
	if completed.Summary.NotScanned != 0 || completed.Summary.Missing != 2 {
		t.Fatalf("expected not_scanned=0 missing=2 after complete, got %+v", completed.Summary)
	}
}

// TestStockOpnames_NotScannedItemsGroupedByWeight guards the client
// complaint this feature fixes: knowing "3 units left" isn't enough to go
// find them physically — the UI needs the sku/weight of each unscanned
// unit, and a per-weight total (not one grand total across every weight,
// since a 5-gram bar and a 10-gram bar aren't fungible for a physical
// count).
func TestStockOpnames_NotScannedItemsGroupedByWeight(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	category := createCategory(t, adminToken, "Batangan")
	brand := createBrand(t, adminToken, "Antam")
	product5g := createProduct(t, adminToken, "Emas Batangan 5gr", category.ID, brand.ID, 5)
	product10g := createProduct(t, adminToken, "Emas Batangan 10gr", category.ID, brand.ID, 10)

	scanned := createStockItemAPI(t, adminToken, product5g.ID, validStockItemBody(map[string]any{"serial_number": "WEIGHT-SN-1"}))
	_ = createStockItemAPI(t, adminToken, product5g.ID, validStockItemBody(map[string]any{"serial_number": "WEIGHT-SN-2"}))
	_ = createStockItemAPI(t, adminToken, product10g.ID, validStockItemBody(map[string]any{"serial_number": "WEIGHT-SN-3"}))

	opname := createStockOpname(t, adminToken)

	// Before any scan: 2x 5-gram + 1x 10-gram, all still unscanned.
	status, resp := doRequest(t, http.MethodGet, "/api/stock-opnames/"+opname.ID, nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("get: expected 200, got %d (resp=%+v)", status, resp)
	}
	var fetched stockOpnameDTO
	decodeData(t, resp, &fetched)
	if len(fetched.NotScannedItems) != 3 {
		t.Fatalf("expected 3 not_scanned_items, got %+v", fetched.NotScannedItems)
	}
	if len(fetched.NotScannedByWeight) != 2 {
		t.Fatalf("expected 2 weight groups (5g, 10g), got %+v", fetched.NotScannedByWeight)
	}
	group5g, group10g := fetched.NotScannedByWeight[0], fetched.NotScannedByWeight[1]
	if group5g.WeightGram != 5 || group5g.Count != 2 || group5g.TotalWeightGram != 10 {
		t.Fatalf("expected 5g group {weight:5 count:2 total:10}, got %+v", group5g)
	}
	if group10g.WeightGram != 10 || group10g.Count != 1 || group10g.TotalWeightGram != 10 {
		t.Fatalf("expected 10g group {weight:10 count:1 total:10}, got %+v", group10g)
	}
	for _, it := range fetched.NotScannedItems {
		if it.SKU == "" || it.ProductName == "" || it.Barcode == "" || it.StockItemID == "" {
			t.Fatalf("expected sku/product_name/barcode/stock_item_id populated, got %+v", it)
		}
	}

	// Scanning one 5-gram unit drops that group's count to 1 — the scan
	// response itself reflects it immediately, without a separate GET.
	status, resp = doRequest(t, http.MethodPost, "/api/stock-opnames/"+opname.ID+"/scan", map[string]any{"barcode": scanned.Barcode}, adminToken)
	if status != http.StatusOK {
		t.Fatalf("scan: expected 200, got %d (resp=%+v)", status, resp)
	}
	var scanResult scanStockOpnameItemDTO
	decodeData(t, resp, &scanResult)
	if len(scanResult.NotScannedItems) != 2 {
		t.Fatalf("expected 2 not_scanned_items after scanning 1 of 3, got %+v", scanResult.NotScannedItems)
	}
	if len(scanResult.NotScannedByWeight) != 2 {
		t.Fatalf("expected both weight groups to remain, got %+v", scanResult.NotScannedByWeight)
	}
	if scanResult.NotScannedByWeight[0].WeightGram != 5 || scanResult.NotScannedByWeight[0].Count != 1 {
		t.Fatalf("expected 5g group to drop to count=1, got %+v", scanResult.NotScannedByWeight[0])
	}

	// Once completed, the pending breakdown is empty — those units are now
	// MISSING items instead of a pending list.
	status, resp = doRequest(t, http.MethodPost, "/api/stock-opnames/"+opname.ID+"/complete", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("complete: expected 200, got %d (resp=%+v)", status, resp)
	}
	var completed stockOpnameDTO
	decodeData(t, resp, &completed)
	if len(completed.NotScannedItems) != 0 || len(completed.NotScannedByWeight) != 0 {
		t.Fatalf("expected empty pending breakdown after complete, got items=%+v weight=%+v", completed.NotScannedItems, completed.NotScannedByWeight)
	}
}

func TestStockOpnames_ScanSoldUnitIsUnexpected(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	stockItem := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(nil))
	markStockItemSold(t, stockItem.ID)

	opname := createStockOpname(t, adminToken)
	status, resp := doRequest(t, http.MethodPost, "/api/stock-opnames/"+opname.ID+"/scan", map[string]any{"barcode": stockItem.Barcode}, adminToken)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (resp=%+v)", status, resp)
	}
	var item stockOpnameItemDTO
	decodeData(t, resp, &item)

	if item.Result != "UNEXPECTED" || item.SystemStatus != "SOLD" {
		t.Fatalf("expected result UNEXPECTED system_status SOLD, got %+v", item)
	}
}

// TestStockOpnames_ScanArchivedUnitIsUnexpected also guards against a
// regression of the stock_opname_items.system_status CHECK constraint,
// which used to only allow ('AVAILABLE', 'SOLD') — scanning a non-AVAILABLE,
// non-SOLD unit (ARCHIVED, or VOID before this fix) would violate that
// constraint and 500 instead of recording a clean UNEXPECTED result.
func TestStockOpnames_ScanArchivedUnitIsUnexpected(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	stockItem := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(nil))

	status, resp := doRequest(t, http.MethodDelete, "/api/stock-items/"+stockItem.ID, nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("archive: expected 200, got %d (resp=%+v)", status, resp)
	}

	opname := createStockOpname(t, adminToken)
	status, resp = doRequest(t, http.MethodPost, "/api/stock-opnames/"+opname.ID+"/scan", map[string]any{"barcode": stockItem.Barcode}, adminToken)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (resp=%+v)", status, resp)
	}
	var item stockOpnameItemDTO
	decodeData(t, resp, &item)

	if item.Result != "UNEXPECTED" || item.SystemStatus != "ARCHIVED" {
		t.Fatalf("expected result UNEXPECTED system_status ARCHIVED, got %+v", item)
	}
}

func TestStockOpnames_ScanUnknownBarcodeNotFound(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	opname := createStockOpname(t, adminToken)

	status, resp := doRequest(t, http.MethodPost, "/api/stock-opnames/"+opname.ID+"/scan", map[string]any{"barcode": "NOT-A-REAL-BARCODE"}, adminToken)
	if status != http.StatusNotFound {
		t.Fatalf("expected 404, got %d (resp=%+v)", status, resp)
	}
}

func TestStockOpnames_ScanEmptyBarcode(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	opname := createStockOpname(t, adminToken)

	status, resp := doRequest(t, http.MethodPost, "/api/stock-opnames/"+opname.ID+"/scan", map[string]any{"barcode": ""}, adminToken)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (resp=%+v)", status, resp)
	}
}

func TestStockOpnames_ScanSameUnitTwiceRejected(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	stockItem := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(nil))
	opname := createStockOpname(t, adminToken)

	status, resp := doRequest(t, http.MethodPost, "/api/stock-opnames/"+opname.ID+"/scan", map[string]any{"barcode": stockItem.Barcode}, adminToken)
	if status != http.StatusOK {
		t.Fatalf("first scan: expected 200, got %d (resp=%+v)", status, resp)
	}

	status, resp = doRequest(t, http.MethodPost, "/api/stock-opnames/"+opname.ID+"/scan", map[string]any{"barcode": stockItem.Barcode}, adminToken)
	if status != http.StatusConflict {
		t.Fatalf("second scan: expected 409, got %d (resp=%+v)", status, resp)
	}
}

func TestStockOpnames_ScanOpnameNotFound(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	status, resp := doRequest(t, http.MethodPost, "/api/stock-opnames/"+nonexistentUUID+"/scan", map[string]any{"barcode": "ANY"}, adminToken)
	if status != http.StatusNotFound {
		t.Fatalf("expected 404, got %d (resp=%+v)", status, resp)
	}
}

func TestStockOpnames_ScanAgainstCompletedSessionRejected(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	stockItem := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(nil))
	opname := createStockOpname(t, adminToken)

	status, resp := doRequest(t, http.MethodPost, "/api/stock-opnames/"+opname.ID+"/complete", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("complete: expected 200, got %d (resp=%+v)", status, resp)
	}

	status, resp = doRequest(t, http.MethodPost, "/api/stock-opnames/"+opname.ID+"/scan", map[string]any{"barcode": stockItem.Barcode}, adminToken)
	if status != http.StatusConflict {
		t.Fatalf("expected 409, got %d (resp=%+v)", status, resp)
	}
}

// --- BE-1103: complete ---

func TestStockOpnames_CompleteRequiresAuth(t *testing.T) {
	resetDB(t)

	status, _ := doRequest(t, http.MethodPost, "/api/stock-opnames/"+nonexistentUUID+"/complete", nil, "")
	if status != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", status)
	}
}

func TestStockOpnames_CompleteNonAdminForbidden(t *testing.T) {
	resetDB(t)
	kasir := seedUser(t, "KASIR", true)
	token := login(t, kasir.Email, kasir.Password)

	status, resp := doRequest(t, http.MethodPost, "/api/stock-opnames/"+nonexistentUUID+"/complete", nil, token)
	if status != http.StatusForbidden {
		t.Fatalf("expected 403, got %d (resp=%+v)", status, resp)
	}
}

func TestStockOpnames_CompleteMarksUnscannedAvailableUnitsMissing(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	scanned := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(map[string]any{"serial_number": "OPN-SN-1"}))
	_ = createStockItemAPI(t, adminToken, product.ID, validStockItemBody(map[string]any{"serial_number": "OPN-SN-2"}))

	opname := createStockOpname(t, adminToken)
	status, resp := doRequest(t, http.MethodPost, "/api/stock-opnames/"+opname.ID+"/scan", map[string]any{"barcode": scanned.Barcode}, adminToken)
	if status != http.StatusOK {
		t.Fatalf("scan: expected 200, got %d (resp=%+v)", status, resp)
	}

	status, resp = doRequest(t, http.MethodPost, "/api/stock-opnames/"+opname.ID+"/complete", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("complete: expected 200, got %d (resp=%+v)", status, resp)
	}
	var completed stockOpnameDTO
	decodeData(t, resp, &completed)

	if completed.Status != "COMPLETED" {
		t.Fatalf("expected status COMPLETED, got %q", completed.Status)
	}
	if completed.Summary.Match != 1 || completed.Summary.Missing != 1 || completed.Summary.Unexpected != 0 {
		t.Fatalf("expected summary match=1 missing=1 unexpected=0, got %+v", completed.Summary)
	}
	if len(completed.Items) != 2 {
		t.Fatalf("expected 2 items total (1 scanned + 1 auto-missing), got %d", len(completed.Items))
	}

	status, resp = doRequest(t, http.MethodGet, "/api/stock-opnames/"+opname.ID, nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("get: expected 200, got %d (resp=%+v)", status, resp)
	}
	var fetched stockOpnameDTO
	decodeData(t, resp, &fetched)
	if fetched.Summary.Missing != 1 {
		t.Fatalf("expected missing=1 on GET after complete, got %+v", fetched.Summary)
	}
}

func TestStockOpnames_CompleteAlreadyCompletedRejected(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	opname := createStockOpname(t, adminToken)

	status, resp := doRequest(t, http.MethodPost, "/api/stock-opnames/"+opname.ID+"/complete", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("first complete: expected 200, got %d (resp=%+v)", status, resp)
	}

	status, resp = doRequest(t, http.MethodPost, "/api/stock-opnames/"+opname.ID+"/complete", nil, adminToken)
	if status != http.StatusConflict {
		t.Fatalf("second complete: expected 409, got %d (resp=%+v)", status, resp)
	}
}

func TestStockOpnames_CompleteNotFound(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	status, resp := doRequest(t, http.MethodPost, "/api/stock-opnames/"+nonexistentUUID+"/complete", nil, adminToken)
	if status != http.StatusNotFound {
		t.Fatalf("expected 404, got %d (resp=%+v)", status, resp)
	}
}

func TestStockOpnames_GetNotFound(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	status, resp := doRequest(t, http.MethodGet, "/api/stock-opnames/"+nonexistentUUID, nil, adminToken)
	if status != http.StatusNotFound {
		t.Fatalf("expected 404, got %d (resp=%+v)", status, resp)
	}
}

func TestStockOpnames_GetInvalidIDFormat(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	status, resp := doRequest(t, http.MethodGet, "/api/stock-opnames/1", nil, adminToken)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (resp=%+v)", status, resp)
	}
}

// --- End-to-end round trip across all three tickets ---

func TestStockOpnames_FullRoundTrip(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)

	var scannedItems []stockItemDTO
	for i := 0; i < 3; i++ {
		scannedItems = append(scannedItems, createStockItemAPI(t, adminToken, product.ID, validStockItemBody(map[string]any{
			"serial_number": fmt.Sprintf("ROUNDTRIP-SN-%d", i),
		})))
	}

	opname := createStockOpname(t, adminToken)
	for i := 0; i < 2; i++ {
		status, resp := doRequest(t, http.MethodPost, "/api/stock-opnames/"+opname.ID+"/scan", map[string]any{"barcode": scannedItems[i].Barcode}, adminToken)
		if status != http.StatusOK {
			t.Fatalf("scan %d: expected 200, got %d (resp=%+v)", i, status, resp)
		}
	}

	status, resp := doRequest(t, http.MethodPost, "/api/stock-opnames/"+opname.ID+"/complete", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("complete: expected 200, got %d (resp=%+v)", status, resp)
	}
	var completed stockOpnameDTO
	decodeData(t, resp, &completed)

	if completed.Summary.Match != 2 || completed.Summary.Missing != 1 || completed.Summary.Unexpected != 0 {
		t.Fatalf("expected match=2 missing=1 unexpected=0, got %+v", completed.Summary)
	}
}
