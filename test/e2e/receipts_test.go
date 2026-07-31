package e2e

import (
	"context"
	"net/http"
	"testing"
)

// receiptPartyDTO is the counterparty (customer or supplier) on a receipt.
type receiptPartyDTO struct {
	Name    string `json:"name"`
	Phone   string `json:"phone"`
	Address string `json:"address"`
}

type receiptStoreDTO struct {
	Name    string `json:"name"`
	Address string `json:"address"`
	Phone   string `json:"phone"`
}

type receiptDTO struct {
	transactionDTO
	Customer   *receiptPartyDTO `json:"customer"`
	Supplier   *receiptPartyDTO `json:"supplier"`
	Store      receiptStoreDTO  `json:"store"`
	InvoiceURL string           `json:"invoice_url"`
}

// seedShopSettings inserts shop_name/shop_address/shop_phone directly via
// SQL (no public write endpoint for settings exists) — attributed to
// whichever user was seeded first in the test, same "insert fixtures
// directly, no endpoint needed" pattern as seedUser itself.
func seedShopSettings(t *testing.T) {
	t.Helper()

	ctx := context.Background()
	var adminID int64
	if err := testPool.QueryRow(ctx, `SELECT id FROM users ORDER BY id LIMIT 1`).Scan(&adminID); err != nil {
		t.Fatalf("seed shop settings: no user to attribute updated_by: %v", err)
	}

	_, err := testPool.Exec(ctx, `
		INSERT INTO settings (key, value, updated_by) VALUES
		('shop_name', 'Toko Emas Sejahtera', $1),
		('shop_address', 'Jl. Testing No. 1, Jakarta', $1),
		('shop_phone', '021-1234567', $1)
	`, adminID)
	if err != nil {
		t.Fatalf("seed shop settings: %v", err)
	}
}

func TestReceipts_RequiresAuth(t *testing.T) {
	resetDB(t)

	status, _ := doRequest(t, http.MethodGet, "/api/transactions/"+nonexistentUUID+"/receipt", nil, "")
	if status != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", status)
	}
}

func TestReceipts_SellTransactionIncludesCustomerAndStore(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	seedShopSettings(t)
	product := stockItemFixtureProduct(t, adminToken)
	stockItem := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(nil))
	customer := createCustomer(t, adminToken, map[string]any{"name": "Budi Santoso", "phone": "0811111111", "address": "Jl. Mawar No. 2"})

	status, resp := doRequest(t, http.MethodPost, "/api/transactions", sellTransactionBody(customer.ID, []map[string]any{
		{"stock_item_id": stockItem.ID, "price_total": 1500000},
	}), adminToken)
	if status != http.StatusCreated {
		t.Fatalf("create sale: expected 201, got %d (resp=%+v)", status, resp)
	}
	var tx transactionDTO
	decodeData(t, resp, &tx)

	status, resp = doRequest(t, http.MethodGet, "/api/transactions/"+tx.ID+"/receipt", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("get receipt: expected 200, got %d (resp=%+v)", status, resp)
	}
	var receipt receiptDTO
	decodeData(t, resp, &receipt)

	if receipt.TransactionCode != tx.TransactionCode || len(receipt.Items) != 1 {
		t.Fatalf("expected receipt to match created transaction, got %+v", receipt)
	}
	if receipt.Customer == nil || receipt.Customer.Name != "Budi Santoso" || receipt.Customer.Phone != "0811111111" || receipt.Customer.Address != "Jl. Mawar No. 2" {
		t.Fatalf("expected customer populated from fixture, got %+v", receipt.Customer)
	}
	if receipt.Supplier != nil {
		t.Fatalf("expected supplier nil for a SELL receipt, got %+v", receipt.Supplier)
	}
	if receipt.Store.Name != "Toko Emas Sejahtera" || receipt.Store.Address != "Jl. Testing No. 1, Jakarta" || receipt.Store.Phone != "021-1234567" {
		t.Fatalf("expected store data from seeded settings, got %+v", receipt.Store)
	}
	expectedInvoiceURL := "/api/transactions/" + tx.ID + "/receipt"
	if receipt.InvoiceURL != expectedInvoiceURL {
		t.Fatalf("expected invoice_url %q, got %q", expectedInvoiceURL, receipt.InvoiceURL)
	}
}

func TestReceipts_BuyTransactionIncludesCustomer(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	seedShopSettings(t)
	product := stockItemFixtureProduct(t, adminToken)
	customer := createCustomer(t, adminToken, map[string]any{"name": "Siti Aminah"})

	status, resp := doRequest(t, http.MethodPost, "/api/transactions", buyTransactionBody(customer.ID, []map[string]any{
		{"product_id": product.ID, "serial_number": "RCPT-BUY-1", "condition": "GOOD", "price_total": 900000},
	}), adminToken)
	if status != http.StatusCreated {
		t.Fatalf("create buy: expected 201, got %d (resp=%+v)", status, resp)
	}
	var tx transactionDTO
	decodeData(t, resp, &tx)

	status, resp = doRequest(t, http.MethodGet, "/api/transactions/"+tx.ID+"/receipt", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("get receipt: expected 200, got %d (resp=%+v)", status, resp)
	}
	var receipt receiptDTO
	decodeData(t, resp, &receipt)

	if receipt.Type != "BUY" || receipt.Customer == nil || receipt.Customer.Name != "Siti Aminah" {
		t.Fatalf("expected BUY receipt with customer populated, got %+v", receipt)
	}
	if receipt.Supplier != nil {
		t.Fatalf("expected supplier nil for a BUY receipt, got %+v", receipt.Supplier)
	}
}

func TestReceipts_SellSupplierTransactionIncludesSupplier(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	seedShopSettings(t)
	product := stockItemFixtureProduct(t, adminToken)
	stockItem := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(nil))
	supplier := createSupplier(t, adminToken, map[string]any{"name": "Toko Emas Jaya", "phone": "0822222222", "address": "Jl. Kenanga No. 3"})

	status, resp := doRequest(t, http.MethodPost, "/api/transactions", map[string]any{
		"type":           "SELL_SUPPLIER",
		"supplier_id":    supplier.ID,
		"payment_method": "CASH",
		"items":          []map[string]any{{"stock_item_id": stockItem.ID, "price_total": 100000}},
	}, adminToken)
	if status != http.StatusCreated {
		t.Fatalf("create sell_supplier: expected 201, got %d (resp=%+v)", status, resp)
	}
	var tx transactionDTO
	decodeData(t, resp, &tx)

	status, resp = doRequest(t, http.MethodGet, "/api/transactions/"+tx.ID+"/receipt", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("get receipt: expected 200, got %d (resp=%+v)", status, resp)
	}
	var receipt receiptDTO
	decodeData(t, resp, &receipt)

	if receipt.Customer != nil {
		t.Fatalf("expected customer nil for a SELL_SUPPLIER receipt, got %+v", receipt.Customer)
	}
	if receipt.Supplier == nil || receipt.Supplier.Name != "Toko Emas Jaya" || receipt.Supplier.Phone != "0822222222" || receipt.Supplier.Address != "Jl. Kenanga No. 3" {
		t.Fatalf("expected supplier populated from fixture, got %+v", receipt.Supplier)
	}
}

func TestReceipts_InvoiceURLCachedAcrossCalls(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	seedShopSettings(t)
	product := stockItemFixtureProduct(t, adminToken)
	stockItem := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(nil))
	customer := createCustomer(t, adminToken, map[string]any{"name": "Budi Santoso"})

	status, resp := doRequest(t, http.MethodPost, "/api/transactions", sellTransactionBody(customer.ID, []map[string]any{
		{"stock_item_id": stockItem.ID, "price_total": 1500000},
	}), adminToken)
	if status != http.StatusCreated {
		t.Fatalf("create sale: expected 201, got %d (resp=%+v)", status, resp)
	}
	var tx transactionDTO
	decodeData(t, resp, &tx)

	// Before the first receipt call, invoice_url must still be NULL.
	var invoiceURLBefore *string
	if err := testPool.QueryRow(context.Background(), `SELECT invoice_url FROM transactions WHERE public_id = $1`, tx.ID).Scan(&invoiceURLBefore); err != nil {
		t.Fatalf("query invoice_url before: %v", err)
	}
	if invoiceURLBefore != nil {
		t.Fatalf("expected invoice_url NULL before first receipt call, got %v", *invoiceURLBefore)
	}

	status, resp = doRequest(t, http.MethodGet, "/api/transactions/"+tx.ID+"/receipt", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("first receipt call: expected 200, got %d (resp=%+v)", status, resp)
	}
	var first receiptDTO
	decodeData(t, resp, &first)

	var invoiceURLAfter *string
	if err := testPool.QueryRow(context.Background(), `SELECT invoice_url FROM transactions WHERE public_id = $1`, tx.ID).Scan(&invoiceURLAfter); err != nil {
		t.Fatalf("query invoice_url after: %v", err)
	}
	if invoiceURLAfter == nil || *invoiceURLAfter != first.InvoiceURL {
		t.Fatalf("expected invoice_url persisted after first call matching response, got db=%v response=%q", invoiceURLAfter, first.InvoiceURL)
	}

	status, resp = doRequest(t, http.MethodGet, "/api/transactions/"+tx.ID+"/receipt", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("second receipt call: expected 200, got %d (resp=%+v)", status, resp)
	}
	var second receiptDTO
	decodeData(t, resp, &second)

	if second.InvoiceURL != first.InvoiceURL {
		t.Fatalf("expected invoice_url stable across calls, first=%q second=%q", first.InvoiceURL, second.InvoiceURL)
	}
}

func TestReceipts_WorksWithoutShopSettingsSeeded(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	// Deliberately no seedShopSettings(t) call here.
	product := stockItemFixtureProduct(t, adminToken)
	stockItem := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(nil))
	customer := createCustomer(t, adminToken, map[string]any{"name": "Budi Santoso"})

	status, resp := doRequest(t, http.MethodPost, "/api/transactions", sellTransactionBody(customer.ID, []map[string]any{
		{"stock_item_id": stockItem.ID, "price_total": 1500000},
	}), adminToken)
	if status != http.StatusCreated {
		t.Fatalf("create sale: expected 201, got %d (resp=%+v)", status, resp)
	}
	var tx transactionDTO
	decodeData(t, resp, &tx)

	status, resp = doRequest(t, http.MethodGet, "/api/transactions/"+tx.ID+"/receipt", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("expected 200 even without seeded settings, got %d (resp=%+v)", status, resp)
	}
	var receipt receiptDTO
	decodeData(t, resp, &receipt)
	if receipt.Store.Name != "" || receipt.Store.Address != "" || receipt.Store.Phone != "" {
		t.Fatalf("expected empty store fields when settings unseeded, got %+v", receipt.Store)
	}
}

func TestReceipts_NotFound(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	status, resp := doRequest(t, http.MethodGet, "/api/transactions/"+nonexistentUUID+"/receipt", nil, adminToken)
	if status != http.StatusNotFound {
		t.Fatalf("expected 404, got %d (resp=%+v)", status, resp)
	}
}

func TestReceipts_InvalidIDFormat(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	status, resp := doRequest(t, http.MethodGet, "/api/transactions/1/receipt", nil, adminToken)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (resp=%+v)", status, resp)
	}
}
