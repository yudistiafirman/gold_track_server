package e2e

import (
	"context"
	"net/http"
	"testing"
	"time"
)

type transactionTypeBreakdownDTO struct {
	Type             string  `json:"type"`
	TransactionCount int     `json:"transaction_count"`
	TotalAmount      float64 `json:"total_amount"`
	TotalWeight      float64 `json:"total_weight"`
}

type transactionReportTotalsDTO struct {
	TransactionCount int     `json:"transaction_count"`
	TotalAmount      float64 `json:"total_amount"`
	TotalWeight      float64 `json:"total_weight"`
}

type transactionReportDTO struct {
	From      string                        `json:"from"`
	To        string                        `json:"to"`
	Breakdown []transactionTypeBreakdownDTO `json:"breakdown"`
	Total     transactionReportTotalsDTO    `json:"total"`
}

func breakdownByType(report transactionReportDTO, txType string) *transactionTypeBreakdownDTO {
	for i := range report.Breakdown {
		if report.Breakdown[i].Type == txType {
			return &report.Breakdown[i]
		}
	}
	return nil
}

// setTransactionCreatedAt backdates a transaction's created_at directly via
// SQL — there's no API to do this, same "reach into the DB when no
// endpoint exposes something" pattern as markStockItemSold.
func setTransactionCreatedAt(t *testing.T, publicID string, ts time.Time) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(), `UPDATE transactions SET created_at = $1 WHERE public_id = $2`, ts, publicID); err != nil {
		t.Fatalf("backdate transaction: %v", err)
	}
}

func TestReports_RequireAuth(t *testing.T) {
	resetDB(t)

	paths := []string{"/api/reports/transactions", "/api/reports/stock", "/api/reports/finance"}
	for _, p := range paths {
		status, _ := doRequest(t, http.MethodGet, p, nil, "")
		if status != http.StatusUnauthorized {
			t.Errorf("%s without token: expected 401, got %d", p, status)
		}
	}
}

func TestReports_NonAdminForbidden(t *testing.T) {
	resetDB(t)
	kasir := seedUser(t, "KASIR", true)
	kasirToken := login(t, kasir.Email, kasir.Password)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	paths := []string{"/api/reports/transactions", "/api/reports/stock", "/api/reports/finance"}
	for _, p := range paths {
		for _, tc := range []struct {
			role  string
			token string
		}{{"KASIR", kasirToken}, {"ADMIN", adminToken}} {
			status, resp := doRequest(t, http.MethodGet, p, nil, tc.token)
			if status != http.StatusForbidden {
				t.Errorf("%s as %s: expected 403, got %d (resp=%+v)", p, tc.role, status, resp)
			}
		}
	}
}

// --- BE-1301: transaction report ---

func TestReports_TransactionsBreakdownAllTypes(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	superAdmin := seedUser(t, "SUPER_ADMIN", true)
	superAdminToken := login(t, superAdmin.Email, superAdmin.Password)
	product := stockItemFixtureProduct(t, adminToken) // weight_gram = 10
	customer := createCustomer(t, adminToken, map[string]any{"name": "Budi Santoso"})
	supplier := createSupplier(t, adminToken, map[string]any{"name": "Toko Emas Jaya"})

	sellItem := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(map[string]any{"serial_number": "RPT-SELL-1"}))
	status, resp := doRequest(t, http.MethodPost, "/api/transactions", sellTransactionBody(customer.ID, []map[string]any{
		{"stock_item_id": sellItem.ID, "price_total": 1500000},
	}), adminToken)
	if status != http.StatusCreated {
		t.Fatalf("sell: expected 201, got %d (resp=%+v)", status, resp)
	}

	status, resp = doRequest(t, http.MethodPost, "/api/transactions", buyTransactionBody(customer.ID, []map[string]any{
		{"product_id": product.ID, "serial_number": "RPT-BUY-1", "condition": "GOOD", "price_total": 900000},
	}), adminToken)
	if status != http.StatusCreated {
		t.Fatalf("buy: expected 201, got %d (resp=%+v)", status, resp)
	}

	supplierItem := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(map[string]any{"serial_number": "RPT-SS-1"}))
	status, resp = doRequest(t, http.MethodPost, "/api/transactions", map[string]any{
		"type":           "SELL_SUPPLIER",
		"supplier_id":    supplier.ID,
		"payment_method": "CASH",
		"items":          []map[string]any{{"stock_item_id": supplierItem.ID, "price_total": 100000}},
	}, adminToken)
	if status != http.StatusCreated {
		t.Fatalf("sell_supplier: expected 201, got %d (resp=%+v)", status, resp)
	}

	status, resp = doRequest(t, http.MethodGet, "/api/reports/transactions", nil, superAdminToken)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (resp=%+v)", status, resp)
	}
	var report transactionReportDTO
	decodeData(t, resp, &report)

	sell := breakdownByType(report, "SELL")
	buy := breakdownByType(report, "BUY")
	sellSupplier := breakdownByType(report, "SELL_SUPPLIER")
	if sell == nil || sell.TransactionCount != 1 || sell.TotalAmount != 1500000 || sell.TotalWeight != 10 {
		t.Fatalf("expected SELL breakdown count=1 amount=1500000 weight=10, got %+v", sell)
	}
	if buy == nil || buy.TransactionCount != 1 || buy.TotalAmount != 900000 || buy.TotalWeight != 10 {
		t.Fatalf("expected BUY breakdown count=1 amount=900000 weight=10, got %+v", buy)
	}
	if sellSupplier == nil || sellSupplier.TransactionCount != 1 || sellSupplier.TotalAmount != 100000 || sellSupplier.TotalWeight != 10 {
		t.Fatalf("expected SELL_SUPPLIER breakdown count=1 amount=100000 weight=10, got %+v", sellSupplier)
	}

	wantTotal := transactionReportTotalsDTO{TransactionCount: 3, TotalAmount: 2500000, TotalWeight: 30}
	if report.Total != wantTotal {
		t.Fatalf("expected total %+v, got %+v", wantTotal, report.Total)
	}
}

func TestReports_TransactionsExcludesCancelled(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	superAdmin := seedUser(t, "SUPER_ADMIN", true)
	superAdminToken := login(t, superAdmin.Email, superAdmin.Password)
	product := stockItemFixtureProduct(t, adminToken) // weight_gram = 10
	customer := createCustomer(t, adminToken, map[string]any{"name": "Budi Santoso"})

	keptItem := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(map[string]any{"serial_number": "RPT-KEPT"}))
	status, resp := doRequest(t, http.MethodPost, "/api/transactions", sellTransactionBody(customer.ID, []map[string]any{
		{"stock_item_id": keptItem.ID, "price_total": 1500000},
	}), adminToken)
	if status != http.StatusCreated {
		t.Fatalf("kept sell: expected 201, got %d (resp=%+v)", status, resp)
	}

	cancelledItem := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(map[string]any{"serial_number": "RPT-CANCELLED"}))
	status, resp = doRequest(t, http.MethodPost, "/api/transactions", sellTransactionBody(customer.ID, []map[string]any{
		{"stock_item_id": cancelledItem.ID, "price_total": 9000000},
	}), adminToken)
	if status != http.StatusCreated {
		t.Fatalf("to-be-cancelled sell: expected 201, got %d (resp=%+v)", status, resp)
	}
	var cancelledTx transactionDTO
	decodeData(t, resp, &cancelledTx)

	status, resp = doRequest(t, http.MethodPost, "/api/transactions/"+cancelledTx.ID+"/cancel", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("cancel: expected 200, got %d (resp=%+v)", status, resp)
	}

	status, resp = doRequest(t, http.MethodGet, "/api/reports/transactions", nil, superAdminToken)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (resp=%+v)", status, resp)
	}
	var report transactionReportDTO
	decodeData(t, resp, &report)

	sell := breakdownByType(report, "SELL")
	if sell == nil || sell.TransactionCount != 1 || sell.TotalAmount != 1500000 || sell.TotalWeight != 10 {
		t.Fatalf("expected only the non-cancelled sell counted (count=1 amount=1500000 weight=10), got %+v", sell)
	}
	wantTotal := transactionReportTotalsDTO{TransactionCount: 1, TotalAmount: 1500000, TotalWeight: 10}
	if report.Total != wantTotal {
		t.Fatalf("expected total %+v (cancelled transaction excluded), got %+v", wantTotal, report.Total)
	}
}

func TestReports_TransactionsFilterByType(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	superAdmin := seedUser(t, "SUPER_ADMIN", true)
	superAdminToken := login(t, superAdmin.Email, superAdmin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	customer := createCustomer(t, adminToken, map[string]any{"name": "Budi Santoso"})
	sellItem := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(nil))

	status, resp := doRequest(t, http.MethodPost, "/api/transactions", sellTransactionBody(customer.ID, []map[string]any{
		{"stock_item_id": sellItem.ID, "price_total": 1500000},
	}), adminToken)
	if status != http.StatusCreated {
		t.Fatalf("sell: expected 201, got %d (resp=%+v)", status, resp)
	}
	status, resp = doRequest(t, http.MethodPost, "/api/transactions", buyTransactionBody(customer.ID, []map[string]any{
		{"product_id": product.ID, "serial_number": "RPT-BUY-FILTER", "condition": "GOOD", "price_total": 900000},
	}), adminToken)
	if status != http.StatusCreated {
		t.Fatalf("buy: expected 201, got %d (resp=%+v)", status, resp)
	}

	status, resp = doRequest(t, http.MethodGet, "/api/reports/transactions?type=SELL", nil, superAdminToken)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (resp=%+v)", status, resp)
	}
	var report transactionReportDTO
	decodeData(t, resp, &report)
	if len(report.Breakdown) != 1 || report.Breakdown[0].Type != "SELL" {
		t.Fatalf("expected only SELL breakdown, got %+v", report.Breakdown)
	}
}

func TestReports_TransactionsInvalidType(t *testing.T) {
	resetDB(t)
	superAdmin := seedUser(t, "SUPER_ADMIN", true)
	superAdminToken := login(t, superAdmin.Email, superAdmin.Password)

	status, resp := doRequest(t, http.MethodGet, "/api/reports/transactions?type=INVALID", nil, superAdminToken)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (resp=%+v)", status, resp)
	}
}

func TestReports_TransactionsFilterByDateRange(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	superAdmin := seedUser(t, "SUPER_ADMIN", true)
	superAdminToken := login(t, superAdmin.Email, superAdmin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	customer := createCustomer(t, adminToken, map[string]any{"name": "Budi Santoso"})

	inRangeItem := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(map[string]any{"serial_number": "RPT-DATE-IN"}))
	status, resp := doRequest(t, http.MethodPost, "/api/transactions", sellTransactionBody(customer.ID, []map[string]any{
		{"stock_item_id": inRangeItem.ID, "price_total": 1000000},
	}), adminToken)
	if status != http.StatusCreated {
		t.Fatalf("in-range sell: expected 201, got %d (resp=%+v)", status, resp)
	}
	var inRangeTx transactionDTO
	decodeData(t, resp, &inRangeTx)
	setTransactionCreatedAt(t, inRangeTx.ID, time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC))

	outOfRangeItem := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(map[string]any{"serial_number": "RPT-DATE-OUT"}))
	status, resp = doRequest(t, http.MethodPost, "/api/transactions", sellTransactionBody(customer.ID, []map[string]any{
		{"stock_item_id": outOfRangeItem.ID, "price_total": 2000000},
	}), adminToken)
	if status != http.StatusCreated {
		t.Fatalf("out-of-range sell: expected 201, got %d (resp=%+v)", status, resp)
	}
	var outOfRangeTx transactionDTO
	decodeData(t, resp, &outOfRangeTx)
	setTransactionCreatedAt(t, outOfRangeTx.ID, time.Date(2026, 8, 15, 10, 0, 0, 0, time.UTC))

	status, resp = doRequest(t, http.MethodGet, "/api/reports/transactions?from=2026-07-01&to=2026-07-31", nil, superAdminToken)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (resp=%+v)", status, resp)
	}
	var report transactionReportDTO
	decodeData(t, resp, &report)
	sell := breakdownByType(report, "SELL")
	if sell == nil || sell.TransactionCount != 1 || sell.TotalAmount != 1000000 {
		t.Fatalf("expected only the July transaction, got %+v", sell)
	}
}

func TestReports_TransactionsBadDateFormat(t *testing.T) {
	resetDB(t)
	superAdmin := seedUser(t, "SUPER_ADMIN", true)
	superAdminToken := login(t, superAdmin.Email, superAdmin.Password)

	status, resp := doRequest(t, http.MethodGet, "/api/reports/transactions?from=01-07-2026", nil, superAdminToken)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (resp=%+v)", status, resp)
	}
}

// --- BE-1302: stock report ---

type stockReportProductRefDTO struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	SKU  string `json:"sku"`
}

type stockReportItemDTO struct {
	Product        stockReportProductRefDTO `json:"product"`
	AvailableCount int                      `json:"available_count"`
	GoodCount      int                      `json:"good_count"`
	BadCount       int                      `json:"bad_count"`
	LowStock       bool                     `json:"low_stock"`
}

type stockReportDTO struct {
	Threshold int                  `json:"threshold"`
	Items     []stockReportItemDTO `json:"items"`
}

func findStockReportItem(report stockReportDTO, productID string) *stockReportItemDTO {
	for i := range report.Items {
		if report.Items[i].Product.ID == productID {
			return &report.Items[i]
		}
	}
	return nil
}

func TestReports_StockBreakdownGoodBad(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	superAdmin := seedUser(t, "SUPER_ADMIN", true)
	superAdminToken := login(t, superAdmin.Email, superAdmin.Password)
	product := stockItemFixtureProduct(t, adminToken)

	createStockItemAPI(t, adminToken, product.ID, validStockItemBody(map[string]any{"serial_number": "RPT-STOCK-1", "condition": "GOOD"}))
	createStockItemAPI(t, adminToken, product.ID, validStockItemBody(map[string]any{"serial_number": "RPT-STOCK-2", "condition": "GOOD"}))
	badItem := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(map[string]any{"serial_number": "RPT-STOCK-3", "condition": "BAD"}))
	soldItem := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(map[string]any{"serial_number": "RPT-STOCK-4", "condition": "GOOD"}))
	markStockItemSold(t, soldItem.ID)
	_ = badItem

	status, resp := doRequest(t, http.MethodGet, "/api/reports/stock", nil, superAdminToken)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (resp=%+v)", status, resp)
	}
	var report stockReportDTO
	decodeData(t, resp, &report)

	item := findStockReportItem(report, product.ID)
	if item == nil {
		t.Fatalf("expected product %s in stock report, got %+v", product.ID, report.Items)
	}
	if item.AvailableCount != 3 || item.GoodCount != 2 || item.BadCount != 1 {
		t.Fatalf("expected available=3 good=2 bad=1 (SOLD unit excluded), got %+v", item)
	}
}

func TestReports_StockZeroStockProductStillAppears(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	superAdmin := seedUser(t, "SUPER_ADMIN", true)
	superAdminToken := login(t, superAdmin.Email, superAdmin.Password)
	category := createCategory(t, adminToken, "Cincin")
	brand := createBrand(t, adminToken, "Antam")
	product := createProduct(t, adminToken, "Cincin Emas 5gr", category.ID, brand.ID, 5)

	status, resp := doRequest(t, http.MethodGet, "/api/reports/stock", nil, superAdminToken)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (resp=%+v)", status, resp)
	}
	var report stockReportDTO
	decodeData(t, resp, &report)

	item := findStockReportItem(report, product.ID)
	if item == nil {
		t.Fatalf("expected zero-stock product %s to still appear, got %+v", product.ID, report.Items)
	}
	if item.AvailableCount != 0 || item.GoodCount != 0 || item.BadCount != 0 || !item.LowStock {
		t.Fatalf("expected all-zero counts and low_stock=true, got %+v", item)
	}
}

func TestReports_StockArchivedProductExcluded(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	superAdmin := seedUser(t, "SUPER_ADMIN", true)
	superAdminToken := login(t, superAdmin.Email, superAdmin.Password)
	product := stockItemFixtureProduct(t, adminToken)

	// Archiving is guarded against AVAILABLE stock (BE-204), so this
	// product is left with zero units before archiving.
	status, resp := doRequest(t, http.MethodDelete, "/api/products/"+product.ID, nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("archive product: expected 200, got %d (resp=%+v)", status, resp)
	}

	status, resp = doRequest(t, http.MethodGet, "/api/reports/stock", nil, superAdminToken)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (resp=%+v)", status, resp)
	}
	var report stockReportDTO
	decodeData(t, resp, &report)

	if findStockReportItem(report, product.ID) != nil {
		t.Fatalf("expected archived product excluded from stock report, got %+v", report.Items)
	}
}

func TestReports_StockThresholdOverride(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	superAdmin := seedUser(t, "SUPER_ADMIN", true)
	superAdminToken := login(t, superAdmin.Email, superAdmin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	for i := 0; i < 3; i++ {
		createStockItemAPI(t, adminToken, product.ID, validStockItemBody(map[string]any{"serial_number": "RPT-THRESH-" + string(rune('A'+i))}))
	}

	status, resp := doRequest(t, http.MethodGet, "/api/reports/stock", nil, superAdminToken)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (resp=%+v)", status, resp)
	}
	var defaultReport stockReportDTO
	decodeData(t, resp, &defaultReport)
	if defaultReport.Threshold != 5 {
		t.Fatalf("expected default threshold=5, got %d", defaultReport.Threshold)
	}
	item := findStockReportItem(defaultReport, product.ID)
	if item == nil || !item.LowStock {
		t.Fatalf("expected low_stock=true with 3 available <= default threshold 5, got %+v", item)
	}

	status, resp = doRequest(t, http.MethodGet, "/api/reports/stock?threshold=2", nil, superAdminToken)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (resp=%+v)", status, resp)
	}
	var overrideReport stockReportDTO
	decodeData(t, resp, &overrideReport)
	if overrideReport.Threshold != 2 {
		t.Fatalf("expected threshold=2, got %d", overrideReport.Threshold)
	}
	item = findStockReportItem(overrideReport, product.ID)
	if item == nil || item.LowStock {
		t.Fatalf("expected low_stock=false with 3 available > threshold 2, got %+v", item)
	}
}

// --- BE-1303: finance report ---

type salesTypeProfitDTO struct {
	Type             string  `json:"type"`
	TransactionCount int     `json:"transaction_count"`
	TotalRevenue     float64 `json:"total_revenue"`
	TotalCOGS        float64 `json:"total_cogs"`
	GrossProfit      float64 `json:"gross_profit"`
}

type expenseCategoryBreakdownDTO struct {
	Category    productRefDTO `json:"category"`
	TotalAmount float64       `json:"total_amount"`
}

type financeSummaryDTO struct {
	SalesBreakdown     []salesTypeProfitDTO          `json:"sales_breakdown"`
	ExpenseBreakdown   []expenseCategoryBreakdownDTO `json:"expense_breakdown"`
	TotalRevenue       float64                       `json:"total_revenue"`
	TotalCOGS          float64                       `json:"total_cogs"`
	GrossProfit        float64                       `json:"gross_profit"`
	GrossMarginPercent float64                       `json:"gross_margin_percent"`
	TotalExpenses      float64                       `json:"total_expenses"`
	NetProfit          float64                       `json:"net_profit"`
}

type financeReportDTO struct {
	From string `json:"from"`
	To   string `json:"to"`
	financeSummaryDTO
}

func salesBreakdownByType(report financeReportDTO, txType string) *salesTypeProfitDTO {
	for i := range report.SalesBreakdown {
		if report.SalesBreakdown[i].Type == txType {
			return &report.SalesBreakdown[i]
		}
	}
	return nil
}

func expenseBreakdownByCategory(report financeReportDTO, categoryID string) *expenseCategoryBreakdownDTO {
	for i := range report.ExpenseBreakdown {
		if report.ExpenseBreakdown[i].Category.ID == categoryID {
			return &report.ExpenseBreakdown[i]
		}
	}
	return nil
}

func TestReports_FinanceGrossProfitIncludesSellSupplier(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	superAdmin := seedUser(t, "SUPER_ADMIN", true)
	superAdminToken := login(t, superAdmin.Email, superAdmin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	customer := createCustomer(t, adminToken, map[string]any{"name": "Budi Santoso"})
	supplier := createSupplier(t, adminToken, map[string]any{"name": "Toko Emas Jaya"})

	// purchase_price (cogs) = 1000000 (validStockItemBody default), sold for 1500000.
	sellItem := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(map[string]any{"serial_number": "RPT-FIN-SELL"}))
	status, resp := doRequest(t, http.MethodPost, "/api/transactions", sellTransactionBody(customer.ID, []map[string]any{
		{"stock_item_id": sellItem.ID, "price_total": 1500000},
	}), adminToken)
	if status != http.StatusCreated {
		t.Fatalf("sell: expected 201, got %d (resp=%+v)", status, resp)
	}

	// SELL_SUPPLIER also counts as a sale — its cogs contributes too.
	supplierItem := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(map[string]any{"serial_number": "RPT-FIN-SS"}))
	status, resp = doRequest(t, http.MethodPost, "/api/transactions", map[string]any{
		"type":           "SELL_SUPPLIER",
		"supplier_id":    supplier.ID,
		"payment_method": "CASH",
		"items":          []map[string]any{{"stock_item_id": supplierItem.ID, "price_total": 1200000}},
	}, adminToken)
	if status != http.StatusCreated {
		t.Fatalf("sell_supplier: expected 201, got %d (resp=%+v)", status, resp)
	}

	status, resp = doRequest(t, http.MethodGet, "/api/reports/finance", nil, superAdminToken)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (resp=%+v)", status, resp)
	}
	var report financeReportDTO
	decodeData(t, resp, &report)

	sell := salesBreakdownByType(report, "SELL")
	sellSupplier := salesBreakdownByType(report, "SELL_SUPPLIER")
	if sell == nil || sell.TransactionCount != 1 || sell.TotalRevenue != 1500000 || sell.TotalCOGS != 1000000 || sell.GrossProfit != 500000 {
		t.Fatalf("expected SELL row revenue=1500000 cogs=1000000 profit=500000, got %+v", sell)
	}
	if sellSupplier == nil || sellSupplier.TransactionCount != 1 || sellSupplier.TotalRevenue != 1200000 || sellSupplier.TotalCOGS != 1000000 || sellSupplier.GrossProfit != 200000 {
		t.Fatalf("expected SELL_SUPPLIER row revenue=1200000 cogs=1000000 profit=200000, got %+v", sellSupplier)
	}

	wantTotalRevenue := 1500000.0 + 1200000.0
	wantTotalCOGS := 1000000.0 + 1000000.0
	wantGrossProfit := wantTotalRevenue - wantTotalCOGS
	if report.TotalRevenue != wantTotalRevenue || report.TotalCOGS != wantTotalCOGS || report.GrossProfit != wantGrossProfit {
		t.Fatalf("expected total revenue=%v cogs=%v profit=%v, got revenue=%v cogs=%v profit=%v",
			wantTotalRevenue, wantTotalCOGS, wantGrossProfit, report.TotalRevenue, report.TotalCOGS, report.GrossProfit)
	}

	wantMargin := wantGrossProfit / wantTotalRevenue * 100
	if report.GrossMarginPercent != wantMargin {
		t.Fatalf("expected gross_margin_percent=%v, got %v", wantMargin, report.GrossMarginPercent)
	}
}

func TestReports_FinanceGrossProfitExcludesBuy(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	superAdmin := seedUser(t, "SUPER_ADMIN", true)
	superAdminToken := login(t, superAdmin.Email, superAdmin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	customer := createCustomer(t, adminToken, map[string]any{"name": "Budi Santoso"})

	status, resp := doRequest(t, http.MethodPost, "/api/transactions", buyTransactionBody(customer.ID, []map[string]any{
		{"product_id": product.ID, "serial_number": "RPT-FIN-BUY", "condition": "GOOD", "price_total": 900000},
	}), adminToken)
	if status != http.StatusCreated {
		t.Fatalf("buy: expected 201, got %d (resp=%+v)", status, resp)
	}

	status, resp = doRequest(t, http.MethodGet, "/api/reports/finance", nil, superAdminToken)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (resp=%+v)", status, resp)
	}
	var report financeReportDTO
	decodeData(t, resp, &report)

	if report.TotalRevenue != 0 || report.TotalCOGS != 0 || report.GrossProfit != 0 {
		t.Fatalf("expected revenue=cogs=profit=0 (BUY never contributes, cogs is always NULL), got %+v", report)
	}
	if len(report.SalesBreakdown) != 0 {
		t.Fatalf("expected empty sales_breakdown (BUY never appears), got %+v", report.SalesBreakdown)
	}
	if report.GrossMarginPercent != 0 {
		t.Fatalf("expected gross_margin_percent=0 when there's no revenue, got %v", report.GrossMarginPercent)
	}
}

func TestReports_FinanceExcludesCancelledSale(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	superAdmin := seedUser(t, "SUPER_ADMIN", true)
	superAdminToken := login(t, superAdmin.Email, superAdmin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	customer := createCustomer(t, adminToken, map[string]any{"name": "Budi Santoso"})

	// purchase_price (cogs) = 1000000 (validStockItemBody default), sold for 1500000.
	sellItem := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(map[string]any{"serial_number": "RPT-FIN-KEPT"}))
	status, resp := doRequest(t, http.MethodPost, "/api/transactions", sellTransactionBody(customer.ID, []map[string]any{
		{"stock_item_id": sellItem.ID, "price_total": 1500000},
	}), adminToken)
	if status != http.StatusCreated {
		t.Fatalf("kept sell: expected 201, got %d (resp=%+v)", status, resp)
	}

	cancelledItem := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(map[string]any{"serial_number": "RPT-FIN-CANCELLED"}))
	status, resp = doRequest(t, http.MethodPost, "/api/transactions", sellTransactionBody(customer.ID, []map[string]any{
		{"stock_item_id": cancelledItem.ID, "price_total": 9000000},
	}), adminToken)
	if status != http.StatusCreated {
		t.Fatalf("to-be-cancelled sell: expected 201, got %d (resp=%+v)", status, resp)
	}
	var cancelledTx transactionDTO
	decodeData(t, resp, &cancelledTx)

	status, resp = doRequest(t, http.MethodPost, "/api/transactions/"+cancelledTx.ID+"/cancel", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("cancel: expected 200, got %d (resp=%+v)", status, resp)
	}

	status, resp = doRequest(t, http.MethodGet, "/api/reports/finance", nil, superAdminToken)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (resp=%+v)", status, resp)
	}
	var report financeReportDTO
	decodeData(t, resp, &report)

	sell := salesBreakdownByType(report, "SELL")
	if sell == nil || sell.TransactionCount != 1 || sell.TotalRevenue != 1500000 || sell.TotalCOGS != 1000000 || sell.GrossProfit != 500000 {
		t.Fatalf("expected only the non-cancelled sell counted (revenue=1500000 cogs=1000000 profit=500000), got %+v", sell)
	}
	if report.TotalRevenue != 1500000 || report.TotalCOGS != 1000000 || report.GrossProfit != 500000 {
		t.Fatalf("expected cancelled sale excluded from totals (revenue=1500000 cogs=1000000 profit=500000), got revenue=%v cogs=%v profit=%v",
			report.TotalRevenue, report.TotalCOGS, report.GrossProfit)
	}
}

func TestReports_FinanceNetProfitIncludesExpenses(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	superAdmin := seedUser(t, "SUPER_ADMIN", true)
	superAdminToken := login(t, superAdmin.Email, superAdmin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	customer := createCustomer(t, adminToken, map[string]any{"name": "Budi Santoso"})
	sellItem := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(nil))

	status, resp := doRequest(t, http.MethodPost, "/api/transactions", sellTransactionBody(customer.ID, []map[string]any{
		{"stock_item_id": sellItem.ID, "price_total": 1500000},
	}), adminToken)
	if status != http.StatusCreated {
		t.Fatalf("sell: expected 201, got %d (resp=%+v)", status, resp)
	}

	category := createExpenseCategory(t, adminToken, "Listrik")
	createExpenseAPI(t, adminToken, validExpenseBody(category.ID, map[string]any{"amount": 200000}))

	status, resp = doRequest(t, http.MethodGet, "/api/reports/finance", nil, superAdminToken)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (resp=%+v)", status, resp)
	}
	var report financeReportDTO
	decodeData(t, resp, &report)

	wantGrossProfit := 1500000.0 - 1000000.0
	wantNetProfit := wantGrossProfit - 200000.0
	if report.GrossProfit != wantGrossProfit || report.TotalExpenses != 200000 || report.NetProfit != wantNetProfit {
		t.Fatalf("expected gross=%v expenses=200000 net=%v, got %+v", wantGrossProfit, wantNetProfit, report)
	}

	expenseRow := expenseBreakdownByCategory(report, category.ID)
	if expenseRow == nil || expenseRow.TotalAmount != 200000 || expenseRow.Category.Name != "Listrik" {
		t.Fatalf("expected expense_breakdown row for Listrik=200000, got %+v", expenseRow)
	}
}

func TestReports_FinanceExpenseBreakdownGroupsByCategory(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	superAdmin := seedUser(t, "SUPER_ADMIN", true)
	superAdminToken := login(t, superAdmin.Email, superAdmin.Password)

	listrik := createExpenseCategory(t, adminToken, "Listrik")
	gaji := createExpenseCategory(t, adminToken, "Gaji Karyawan")

	createExpenseAPI(t, adminToken, validExpenseBody(listrik.ID, map[string]any{"amount": 150000}))
	createExpenseAPI(t, adminToken, validExpenseBody(listrik.ID, map[string]any{"amount": 50000}))
	createExpenseAPI(t, adminToken, validExpenseBody(gaji.ID, map[string]any{"amount": 3000000}))

	status, resp := doRequest(t, http.MethodGet, "/api/reports/finance", nil, superAdminToken)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (resp=%+v)", status, resp)
	}
	var report financeReportDTO
	decodeData(t, resp, &report)

	if len(report.ExpenseBreakdown) != 2 {
		t.Fatalf("expected 2 category rows, got %+v", report.ExpenseBreakdown)
	}
	listrikRow := expenseBreakdownByCategory(report, listrik.ID)
	gajiRow := expenseBreakdownByCategory(report, gaji.ID)
	if listrikRow == nil || listrikRow.TotalAmount != 200000 {
		t.Fatalf("expected Listrik total=200000 (two expenses summed into one row), got %+v", listrikRow)
	}
	if gajiRow == nil || gajiRow.TotalAmount != 3000000 {
		t.Fatalf("expected Gaji Karyawan total=3000000, got %+v", gajiRow)
	}
	if report.TotalExpenses != 3200000 {
		t.Fatalf("expected total_expenses=3200000, got %v", report.TotalExpenses)
	}
}

func TestReports_FinanceMarginPercentZeroWhenNoSales(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	superAdmin := seedUser(t, "SUPER_ADMIN", true)
	superAdminToken := login(t, superAdmin.Email, superAdmin.Password)
	category := createExpenseCategory(t, adminToken, "Sewa Tempat")
	createExpenseAPI(t, adminToken, validExpenseBody(category.ID, map[string]any{"amount": 500000}))

	status, resp := doRequest(t, http.MethodGet, "/api/reports/finance", nil, superAdminToken)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (resp=%+v)", status, resp)
	}
	var report financeReportDTO
	decodeData(t, resp, &report)

	if report.TotalRevenue != 0 || report.GrossProfit != 0 {
		t.Fatalf("expected no revenue/profit with zero sales, got %+v", report)
	}
	if report.GrossMarginPercent != 0 {
		t.Fatalf("expected gross_margin_percent=0 (not NaN/Inf) when there's no revenue, got %v", report.GrossMarginPercent)
	}
	if report.TotalExpenses != 500000 || report.NetProfit != -500000 {
		t.Fatalf("expected total_expenses=500000 net_profit=-500000, got %+v", report)
	}
}

func TestReports_FinanceFilterByDateRange(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	superAdmin := seedUser(t, "SUPER_ADMIN", true)
	superAdminToken := login(t, superAdmin.Email, superAdmin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	customer := createCustomer(t, adminToken, map[string]any{"name": "Budi Santoso"})

	inRangeItem := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(map[string]any{"serial_number": "RPT-FIN-DATE-IN"}))
	status, resp := doRequest(t, http.MethodPost, "/api/transactions", sellTransactionBody(customer.ID, []map[string]any{
		{"stock_item_id": inRangeItem.ID, "price_total": 1500000},
	}), adminToken)
	if status != http.StatusCreated {
		t.Fatalf("in-range sell: expected 201, got %d (resp=%+v)", status, resp)
	}
	var inRangeTx transactionDTO
	decodeData(t, resp, &inRangeTx)
	setTransactionCreatedAt(t, inRangeTx.ID, time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC))

	outOfRangeItem := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(map[string]any{"serial_number": "RPT-FIN-DATE-OUT"}))
	status, resp = doRequest(t, http.MethodPost, "/api/transactions", sellTransactionBody(customer.ID, []map[string]any{
		{"stock_item_id": outOfRangeItem.ID, "price_total": 3000000},
	}), adminToken)
	if status != http.StatusCreated {
		t.Fatalf("out-of-range sell: expected 201, got %d (resp=%+v)", status, resp)
	}
	var outOfRangeTx transactionDTO
	decodeData(t, resp, &outOfRangeTx)
	setTransactionCreatedAt(t, outOfRangeTx.ID, time.Date(2026, 8, 15, 10, 0, 0, 0, time.UTC))

	category := createExpenseCategory(t, adminToken, "Sewa Tempat")
	createExpenseAPI(t, adminToken, validExpenseBody(category.ID, map[string]any{"amount": 100000, "expense_date": "2026-07-10"}))
	createExpenseAPI(t, adminToken, validExpenseBody(category.ID, map[string]any{"amount": 999000, "expense_date": "2026-08-10"}))

	status, resp = doRequest(t, http.MethodGet, "/api/reports/finance?from=2026-07-01&to=2026-07-31", nil, superAdminToken)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (resp=%+v)", status, resp)
	}
	var report financeReportDTO
	decodeData(t, resp, &report)

	wantGrossProfit := 1500000.0 - 1000000.0
	if report.GrossProfit != wantGrossProfit || report.TotalExpenses != 100000 {
		t.Fatalf("expected only July data (gross=%v expenses=100000), got %+v", wantGrossProfit, report)
	}
}

func TestReports_FinanceBadDateFormat(t *testing.T) {
	resetDB(t)
	superAdmin := seedUser(t, "SUPER_ADMIN", true)
	superAdminToken := login(t, superAdmin.Email, superAdmin.Password)

	status, resp := doRequest(t, http.MethodGet, "/api/reports/finance?to=31-07-2026", nil, superAdminToken)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (resp=%+v)", status, resp)
	}
}

// --- GET /api/reports/dashboard ---

type pendingPurchaseOrderDTO struct {
	ID           string  `json:"id"`
	POCode       string  `json:"po_code"`
	SupplierName string  `json:"supplier_name"`
	TotalAmount  float64 `json:"total_amount"`
}

type cashSummaryDTO struct {
	TotalGoldValue     float64 `json:"total_gold_value"`
	TotalBalance       float64 `json:"total_balance"`
	TotalExternalFunds float64 `json:"total_external_funds"`
	TotalExternalDebts float64 `json:"total_external_debts"`
}

type dashboardDTO struct {
	From                       string                        `json:"from"`
	To                         string                        `json:"to"`
	Finance                    financeSummaryDTO             `json:"finance"`
	TransactionBreakdown       []transactionTypeBreakdownDTO `json:"transaction_breakdown"`
	TransactionTotal           transactionReportTotalsDTO    `json:"transaction_total"`
	LowStockThreshold          int                           `json:"low_stock_threshold"`
	LowStockItems              []stockReportItemDTO          `json:"low_stock_items"`
	PendingPurchaseOrders      []pendingPurchaseOrderDTO     `json:"pending_purchase_orders"`
	PendingPurchaseOrdersTotal int                           `json:"pending_purchase_orders_total"`
	CashSummary                cashSummaryDTO                `json:"cash_summary"`
}

func TestReports_DashboardRequiresAuth(t *testing.T) {
	resetDB(t)

	status, _ := doRequest(t, http.MethodGet, "/api/reports/dashboard", nil, "")
	if status != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", status)
	}
}

func TestReports_DashboardNonAdminForbidden(t *testing.T) {
	resetDB(t)
	kasir := seedUser(t, "KASIR", true)
	kasirToken := login(t, kasir.Email, kasir.Password)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	for _, tc := range []struct {
		role  string
		token string
	}{{"KASIR", kasirToken}, {"ADMIN", adminToken}} {
		status, resp := doRequest(t, http.MethodGet, "/api/reports/dashboard", nil, tc.token)
		if status != http.StatusForbidden {
			t.Fatalf("as %s: expected 403, got %d (resp=%+v)", tc.role, status, resp)
		}
	}
}

func TestReports_DashboardDefaultsToCurrentMonth(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	superAdmin := seedUser(t, "SUPER_ADMIN", true)
	superAdminToken := login(t, superAdmin.Email, superAdmin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	customer := createCustomer(t, adminToken, map[string]any{"name": "Budi Santoso"})

	thisMonthItem := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(map[string]any{"serial_number": "DASH-THIS-MONTH"}))
	status, resp := doRequest(t, http.MethodPost, "/api/transactions", sellTransactionBody(customer.ID, []map[string]any{
		{"stock_item_id": thisMonthItem.ID, "price_total": 1500000},
	}), adminToken)
	if status != http.StatusCreated {
		t.Fatalf("this-month sell: expected 201, got %d (resp=%+v)", status, resp)
	}

	lastMonthItem := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(map[string]any{"serial_number": "DASH-LAST-MONTH"}))
	status, resp = doRequest(t, http.MethodPost, "/api/transactions", sellTransactionBody(customer.ID, []map[string]any{
		{"stock_item_id": lastMonthItem.ID, "price_total": 3000000},
	}), adminToken)
	if status != http.StatusCreated {
		t.Fatalf("last-month sell: expected 201, got %d (resp=%+v)", status, resp)
	}
	var lastMonthTx transactionDTO
	decodeData(t, resp, &lastMonthTx)
	lastMonth := time.Now().AddDate(0, -1, 0)
	setTransactionCreatedAt(t, lastMonthTx.ID, lastMonth)

	status, resp = doRequest(t, http.MethodGet, "/api/reports/dashboard", nil, superAdminToken)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (resp=%+v)", status, resp)
	}
	var dashboard dashboardDTO
	decodeData(t, resp, &dashboard)

	nowUTC := time.Now().UTC()
	firstOfMonth := time.Date(nowUTC.Year(), nowUTC.Month(), 1, 0, 0, 0, 0, time.UTC)
	wantFrom := firstOfMonth.Format("2006-01-02")
	wantTo := nowUTC.Format("2006-01-02")
	if dashboard.From != wantFrom || dashboard.To != wantTo {
		t.Fatalf("expected default range %s..%s, got %s..%s", wantFrom, wantTo, dashboard.From, dashboard.To)
	}
	if dashboard.TransactionTotal.TransactionCount != 1 || dashboard.TransactionTotal.TotalAmount != 1500000 {
		t.Fatalf("expected only the this-month transaction counted, got %+v", dashboard.TransactionTotal)
	}
}

func TestReports_DashboardDateRangeOverride(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	superAdmin := seedUser(t, "SUPER_ADMIN", true)
	superAdminToken := login(t, superAdmin.Email, superAdmin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	customer := createCustomer(t, adminToken, map[string]any{"name": "Budi Santoso"})

	item := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(nil))
	status, resp := doRequest(t, http.MethodPost, "/api/transactions", sellTransactionBody(customer.ID, []map[string]any{
		{"stock_item_id": item.ID, "price_total": 1500000},
	}), adminToken)
	if status != http.StatusCreated {
		t.Fatalf("sell: expected 201, got %d (resp=%+v)", status, resp)
	}
	var tx transactionDTO
	decodeData(t, resp, &tx)
	setTransactionCreatedAt(t, tx.ID, time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC))

	status, resp = doRequest(t, http.MethodGet, "/api/reports/dashboard?from=2026-07-01&to=2026-07-31", nil, superAdminToken)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (resp=%+v)", status, resp)
	}
	var dashboard dashboardDTO
	decodeData(t, resp, &dashboard)
	if dashboard.From != "2026-07-01" || dashboard.To != "2026-07-31" {
		t.Fatalf("expected echoed override range, got %s..%s", dashboard.From, dashboard.To)
	}
	if dashboard.TransactionTotal.TransactionCount != 1 {
		t.Fatalf("expected the backdated July transaction counted, got %+v", dashboard.TransactionTotal)
	}
}

func TestReports_DashboardFinanceAndTransactionsMatchStandaloneReports(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	superAdmin := seedUser(t, "SUPER_ADMIN", true)
	superAdminToken := login(t, superAdmin.Email, superAdmin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	customer := createCustomer(t, adminToken, map[string]any{"name": "Budi Santoso"})
	item := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(nil))

	status, resp := doRequest(t, http.MethodPost, "/api/transactions", sellTransactionBody(customer.ID, []map[string]any{
		{"stock_item_id": item.ID, "price_total": 1500000},
	}), adminToken)
	if status != http.StatusCreated {
		t.Fatalf("sell: expected 201, got %d (resp=%+v)", status, resp)
	}
	category := createExpenseCategory(t, adminToken, "Listrik")
	createExpenseAPI(t, adminToken, validExpenseBody(category.ID, map[string]any{"amount": 200000}))

	status, resp = doRequest(t, http.MethodGet, "/api/reports/dashboard", nil, superAdminToken)
	if status != http.StatusOK {
		t.Fatalf("dashboard: expected 200, got %d (resp=%+v)", status, resp)
	}
	var dashboard dashboardDTO
	decodeData(t, resp, &dashboard)

	status, resp = doRequest(t, http.MethodGet, "/api/reports/finance?from="+dashboard.From+"&to="+dashboard.To, nil, superAdminToken)
	if status != http.StatusOK {
		t.Fatalf("finance: expected 200, got %d (resp=%+v)", status, resp)
	}
	var finance financeReportDTO
	decodeData(t, resp, &finance)
	if dashboard.Finance.GrossProfit != finance.GrossProfit || dashboard.Finance.TotalExpenses != finance.TotalExpenses || dashboard.Finance.NetProfit != finance.NetProfit {
		t.Fatalf("expected dashboard finance to match standalone report, got dashboard=%+v standalone=%+v", dashboard.Finance, finance)
	}

	status, resp = doRequest(t, http.MethodGet, "/api/reports/transactions?from="+dashboard.From+"&to="+dashboard.To, nil, superAdminToken)
	if status != http.StatusOK {
		t.Fatalf("transactions: expected 200, got %d (resp=%+v)", status, resp)
	}
	var txReport transactionReportDTO
	decodeData(t, resp, &txReport)
	if dashboard.TransactionTotal != txReport.Total {
		t.Fatalf("expected dashboard transaction_total to match standalone report, got dashboard=%+v standalone=%+v", dashboard.TransactionTotal, txReport.Total)
	}
}

func TestReports_DashboardLowStockItemsOnly(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	superAdmin := seedUser(t, "SUPER_ADMIN", true)
	superAdminToken := login(t, superAdmin.Email, superAdmin.Password)

	lowStockProduct := stockItemFixtureProduct(t, adminToken)
	createStockItemAPI(t, adminToken, lowStockProduct.ID, validStockItemBody(map[string]any{"serial_number": "DASH-LOW-1"}))

	category := createCategory(t, adminToken, "Cincin")
	brand := createBrand(t, adminToken, "UBS")
	plentyProduct := createProduct(t, adminToken, "Cincin Emas 5gr", category.ID, brand.ID, 5)
	for i := 0; i < 10; i++ {
		createStockItemAPI(t, adminToken, plentyProduct.ID, validStockItemBody(map[string]any{"serial_number": "DASH-PLENTY-" + string(rune('A'+i))}))
	}

	status, resp := doRequest(t, http.MethodGet, "/api/reports/dashboard?threshold=5", nil, superAdminToken)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (resp=%+v)", status, resp)
	}
	var dashboard dashboardDTO
	decodeData(t, resp, &dashboard)

	if dashboard.LowStockThreshold != 5 {
		t.Fatalf("expected threshold=5, got %d", dashboard.LowStockThreshold)
	}
	foundLow, foundPlenty := false, false
	for _, item := range dashboard.LowStockItems {
		if item.Product.ID == lowStockProduct.ID {
			foundLow = true
		}
		if item.Product.ID == plentyProduct.ID {
			foundPlenty = true
		}
	}
	if !foundLow {
		t.Fatalf("expected the low-stock product to appear, got %+v", dashboard.LowStockItems)
	}
	if foundPlenty {
		t.Fatalf("expected the well-stocked product excluded from low_stock_items, got %+v", dashboard.LowStockItems)
	}
}

func TestReports_DashboardPendingPurchaseOrders(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	superAdmin := seedUser(t, "SUPER_ADMIN", true)
	superAdminToken := login(t, superAdmin.Email, superAdmin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	supplier := createSupplier(t, adminToken, map[string]any{"name": "Toko Emas Jaya"})

	pendingPO := createPurchaseOrder(t, adminToken, supplier.ID, []map[string]any{
		{"product_id": product.ID, "quantity": 1, "purchase_price": 800000},
	})

	cancelledPO := createPurchaseOrder(t, adminToken, supplier.ID, []map[string]any{
		{"product_id": product.ID, "quantity": 1, "purchase_price": 800000},
	})
	status, resp := doRequest(t, http.MethodPost, "/api/purchase-orders/"+cancelledPO.ID+"/cancel", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("cancel: expected 200, got %d (resp=%+v)", status, resp)
	}

	status, resp = doRequest(t, http.MethodGet, "/api/reports/dashboard", nil, superAdminToken)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (resp=%+v)", status, resp)
	}
	var dashboard dashboardDTO
	decodeData(t, resp, &dashboard)

	if dashboard.PendingPurchaseOrdersTotal != 1 || len(dashboard.PendingPurchaseOrders) != 1 {
		t.Fatalf("expected exactly 1 pending PO (cancelled one excluded), got total=%d items=%+v",
			dashboard.PendingPurchaseOrdersTotal, dashboard.PendingPurchaseOrders)
	}
	if dashboard.PendingPurchaseOrders[0].ID != pendingPO.ID || dashboard.PendingPurchaseOrders[0].POCode != pendingPO.POCode {
		t.Fatalf("expected the still-pending PO, got %+v", dashboard.PendingPurchaseOrders[0])
	}
}

func TestReports_DashboardPendingPurchaseOrdersCapAndTotal(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	superAdmin := seedUser(t, "SUPER_ADMIN", true)
	superAdminToken := login(t, superAdmin.Email, superAdmin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	supplier := createSupplier(t, adminToken, map[string]any{"name": "Toko Emas Jaya"})

	for i := 0; i < 7; i++ {
		createPurchaseOrder(t, adminToken, supplier.ID, []map[string]any{
			{"product_id": product.ID, "quantity": 1, "purchase_price": 800000},
		})
	}

	status, resp := doRequest(t, http.MethodGet, "/api/reports/dashboard?pending_limit=3", nil, superAdminToken)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (resp=%+v)", status, resp)
	}
	var dashboard dashboardDTO
	decodeData(t, resp, &dashboard)

	if len(dashboard.PendingPurchaseOrders) != 3 {
		t.Fatalf("expected 3 items capped by pending_limit, got %d", len(dashboard.PendingPurchaseOrders))
	}
	if dashboard.PendingPurchaseOrdersTotal != 7 {
		t.Fatalf("expected total=7 (true count beyond the cap), got %d", dashboard.PendingPurchaseOrdersTotal)
	}
}

// --- dashboard cash_summary (no standalone endpoint — folded into the
// dashboard only, so the FE hits one endpoint for the whole page) ---

func TestReports_DashboardCashSummary(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	superAdmin := seedUser(t, "SUPER_ADMIN", true)
	superAdminToken := login(t, superAdmin.Email, superAdmin.Password)
	product := stockItemFixtureProduct(t, adminToken)

	// AVAILABLE unit contributes to total_gold_value...
	createStockItemAPI(t, adminToken, product.ID, validStockItemBody(map[string]any{
		"serial_number": "CASH-AVAIL", "purchase_price": 10000000,
	}))
	// ...a SOLD unit with a different price must be excluded.
	soldItem := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(map[string]any{
		"serial_number": "CASH-SOLD", "purchase_price": 99999999,
	}))
	markStockItemSold(t, soldItem.ID)

	createBalanceAccount(t, superAdminToken, "BCA Bisnis", 5000000)
	createBalanceAccount(t, superAdminToken, "Cash", 1000000)
	createExternalFund(t, superAdminToken, "Eliza Buyback 2 gram", 5000000)
	createExternalDebt(t, superAdminToken, "Budi", 2000000)

	status, resp := doRequest(t, http.MethodGet, "/api/reports/dashboard", nil, superAdminToken)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (resp=%+v)", status, resp)
	}
	var dashboard dashboardDTO
	decodeData(t, resp, &dashboard)
	cash := dashboard.CashSummary

	if cash.TotalGoldValue != 10000000 {
		t.Fatalf("expected total_gold_value=10000000 (SOLD unit excluded), got %v", cash.TotalGoldValue)
	}
	if cash.TotalBalance != 6000000 {
		t.Fatalf("expected total_balance=6000000, got %v", cash.TotalBalance)
	}
	if cash.TotalExternalFunds != 5000000 {
		t.Fatalf("expected total_external_funds=5000000, got %v", cash.TotalExternalFunds)
	}
	if cash.TotalExternalDebts != 2000000 {
		t.Fatalf("expected total_external_debts=2000000, got %v", cash.TotalExternalDebts)
	}
}
