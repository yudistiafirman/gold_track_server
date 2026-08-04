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

type stockOpnameSummaryCountsDTO struct {
	Match      int `json:"match"`
	Missing    int `json:"missing"`
	Unexpected int `json:"unexpected"`
}

type stockOpnameDTO struct {
	ID         string                      `json:"id"`
	OpnameCode string                      `json:"opname_code"`
	OpnameDate string                      `json:"opname_date"`
	Status     string                      `json:"status"`
	Notes      string                      `json:"notes"`
	Items      []stockOpnameItemDTO        `json:"items"`
	Summary    stockOpnameSummaryCountsDTO `json:"summary"`
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
