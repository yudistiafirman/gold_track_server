package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

type transactionItemDTO struct {
	ID           string  `json:"id"`
	StockItemID  string  `json:"stock_item_id"`
	Barcode      string  `json:"barcode"`
	SerialNumber string  `json:"serial_number"`
	ProductName  string  `json:"product_name"`
	WeightGram   float64 `json:"weight_gram"`
	PricePerGram float64 `json:"price_per_gram"`
	PriceTotal   float64 `json:"price_total"`
}

type transactionSummaryDTO struct {
	ID              string  `json:"id"`
	TransactionCode string  `json:"transaction_code"`
	Type            string  `json:"type"`
	TotalAmount     float64 `json:"total_amount"`
	TotalWeight     float64 `json:"total_weight"`
	PaymentMethod   string  `json:"payment_method"`
	PaymentRef      string  `json:"payment_ref"`
	Status          string  `json:"status"`
}

type transactionListDTO struct {
	Items      []transactionSummaryDTO `json:"items"`
	Pagination paginationDTO           `json:"pagination"`
}

type transactionListItemDTO struct {
	ID              string         `json:"id"`
	TransactionCode string         `json:"transaction_code"`
	Type            string         `json:"type"`
	TotalAmount     float64        `json:"total_amount"`
	TotalWeight     float64        `json:"total_weight"`
	PaymentMethod   string         `json:"payment_method"`
	PaymentRef      string         `json:"payment_ref"`
	Status          string         `json:"status"`
	Customer        *productRefDTO `json:"customer"`
	Supplier        *productRefDTO `json:"supplier"`
}

type generalTransactionListDTO struct {
	Items      []transactionListItemDTO `json:"items"`
	Pagination paginationDTO            `json:"pagination"`
}

type transactionDTO struct {
	ID              string               `json:"id"`
	TransactionCode string               `json:"transaction_code"`
	Type            string               `json:"type"`
	TotalAmount     float64              `json:"total_amount"`
	TotalWeight     float64              `json:"total_weight"`
	PaymentMethod   string               `json:"payment_method"`
	PaymentRef      string               `json:"payment_ref"`
	Status          string               `json:"status"`
	Items           []transactionItemDTO `json:"items"`
}

func sellTransactionBody(customerID string, items []map[string]any) map[string]any {
	return map[string]any{
		"type":           "SELL",
		"customer_id":    customerID,
		"payment_method": "CASH",
		"items":          items,
	}
}

func TestTransactions_CreateRequiresAuth(t *testing.T) {
	resetDB(t)

	status, _ := doRequest(t, http.MethodPost, "/api/transactions", nil, "")
	if status != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", status)
	}
}

func TestTransactions_CreateSellToCustomer(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken) // weight_gram = 10
	stockItem := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(nil))
	customer := createCustomer(t, adminToken, map[string]any{"name": "Budi Santoso"})

	status, resp := doRequest(t, http.MethodPost, "/api/transactions", sellTransactionBody(customer.ID, []map[string]any{
		{"stock_item_id": stockItem.ID, "price_total": 1500000},
	}), adminToken)
	if status != http.StatusCreated {
		t.Fatalf("expected 201, got %d (resp=%+v)", status, resp)
	}
	var tx transactionDTO
	decodeData(t, resp, &tx)

	today := time.Now().Format("20060102")
	expectedCode := "TRX-" + today + "-0001"
	if tx.TransactionCode != expectedCode {
		t.Fatalf("expected transaction_code %q, got %q", expectedCode, tx.TransactionCode)
	}
	if tx.TotalAmount != 1500000 {
		t.Fatalf("expected total_amount=1500000, got %v", tx.TotalAmount)
	}
	if tx.TotalWeight != 10 {
		t.Fatalf("expected total_weight=10, got %v", tx.TotalWeight)
	}
	if len(tx.Items) != 1 || tx.Items[0].PricePerGram != 150000 {
		t.Fatalf("expected price_per_gram=150000, got %+v", tx.Items)
	}

	status, resp = doRequest(t, http.MethodGet, "/api/stock-items/"+stockItem.ID, nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("get stock item: expected 200, got %d (resp=%+v)", status, resp)
	}
	var fetched stockItemDTO
	decodeData(t, resp, &fetched)
	if fetched.Status != "SOLD" {
		t.Fatalf("expected unit status SOLD after sale, got %q", fetched.Status)
	}
}

func TestTransactions_CreateSellArchivedUnitRejected(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	stockItem := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(nil))
	customer := createCustomer(t, adminToken, map[string]any{"name": "Budi Santoso"})

	status, resp := doRequest(t, http.MethodDelete, "/api/stock-items/"+stockItem.ID, nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("archive: expected 200, got %d (resp=%+v)", status, resp)
	}

	status, resp = doRequest(t, http.MethodPost, "/api/transactions", sellTransactionBody(customer.ID, []map[string]any{
		{"stock_item_id": stockItem.ID, "price_total": 1500000},
	}), adminToken)
	if status != http.StatusConflict {
		t.Fatalf("sell archived unit: expected 409, got %d (resp=%+v)", status, resp)
	}
}

func TestTransactions_CreateSellToSupplierRequiresSupplierID(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	stockItem := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(nil))
	customer := createCustomer(t, adminToken, map[string]any{"name": "Budi Santoso"})

	status, resp := doRequest(t, http.MethodPost, "/api/transactions", map[string]any{
		"type":           "SELL_SUPPLIER",
		"payment_method": "CASH",
		"items":          []map[string]any{{"stock_item_id": stockItem.ID, "price_total": 100000}},
	}, adminToken)
	if status != http.StatusBadRequest {
		t.Fatalf("missing supplier_id: expected 400, got %d (resp=%+v)", status, resp)
	}

	status, resp = doRequest(t, http.MethodPost, "/api/transactions", map[string]any{
		"type":           "SELL_SUPPLIER",
		"customer_id":    customer.ID,
		"payment_method": "CASH",
		"items":          []map[string]any{{"stock_item_id": stockItem.ID, "price_total": 100000}},
	}, adminToken)
	if status != http.StatusBadRequest {
		t.Fatalf("customer_id set instead of supplier_id: expected 400, got %d (resp=%+v)", status, resp)
	}
}

func TestTransactions_CreateSellRequiresCustomerID(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	stockItem := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(nil))

	status, resp := doRequest(t, http.MethodPost, "/api/transactions", map[string]any{
		"type":           "SELL",
		"payment_method": "CASH",
		"items":          []map[string]any{{"stock_item_id": stockItem.ID, "price_total": 100000}},
	}, adminToken)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (resp=%+v)", status, resp)
	}
}

func TestTransactions_TransactionCodeIncrementsSameDay(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	item1 := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(map[string]any{"serial_number": "SN-A"}))
	item2 := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(map[string]any{"serial_number": "SN-B"}))
	customer := createCustomer(t, adminToken, map[string]any{"name": "Budi Santoso"})

	status, resp := doRequest(t, http.MethodPost, "/api/transactions", sellTransactionBody(customer.ID, []map[string]any{
		{"stock_item_id": item1.ID, "price_total": 100000},
	}), adminToken)
	if status != http.StatusCreated {
		t.Fatalf("first sale: expected 201, got %d (resp=%+v)", status, resp)
	}
	var first transactionDTO
	decodeData(t, resp, &first)

	status, resp = doRequest(t, http.MethodPost, "/api/transactions", sellTransactionBody(customer.ID, []map[string]any{
		{"stock_item_id": item2.ID, "price_total": 100000},
	}), adminToken)
	if status != http.StatusCreated {
		t.Fatalf("second sale: expected 201, got %d (resp=%+v)", status, resp)
	}
	var second transactionDTO
	decodeData(t, resp, &second)

	today := time.Now().Format("20060102")
	if first.TransactionCode != "TRX-"+today+"-0001" {
		t.Fatalf("expected first code -0001, got %q", first.TransactionCode)
	}
	if second.TransactionCode != "TRX-"+today+"-0002" {
		t.Fatalf("expected second code -0002, got %q", second.TransactionCode)
	}
}

func TestTransactions_CreateRejectsAlreadySoldUnit(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	stockItem := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(nil))
	customer := createCustomer(t, adminToken, map[string]any{"name": "Budi Santoso"})

	body := sellTransactionBody(customer.ID, []map[string]any{
		{"stock_item_id": stockItem.ID, "price_total": 100000},
	})
	status, resp := doRequest(t, http.MethodPost, "/api/transactions", body, adminToken)
	if status != http.StatusCreated {
		t.Fatalf("first sale: expected 201, got %d (resp=%+v)", status, resp)
	}

	status, resp = doRequest(t, http.MethodPost, "/api/transactions", body, adminToken)
	if status != http.StatusConflict {
		t.Fatalf("second sale of same unit: expected 409, got %d (resp=%+v)", status, resp)
	}
}

func TestTransactions_CreateConcurrentDoubleSellOnlyOneWins(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	stockItem := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(nil))
	customer := createCustomer(t, adminToken, map[string]any{"name": "Budi Santoso"})

	body := sellTransactionBody(customer.ID, []map[string]any{
		{"stock_item_id": stockItem.ID, "price_total": 100000},
	})

	// Fire both requests concurrently with a raw http.Client — doRequest
	// calls t.Fatalf internally, which must never happen from a goroutine
	// other than the test's own, so this bypasses it deliberately.
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request body: %v", err)
	}

	var wg sync.WaitGroup
	statuses := make([]int, 2)
	errs := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			req, err := http.NewRequest(http.MethodPost, baseURL+"/api/transactions", bytes.NewReader(bodyBytes))
			if err != nil {
				errs[idx] = err
				return
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+adminToken)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				errs[idx] = err
				return
			}
			defer resp.Body.Close()
			statuses[idx] = resp.StatusCode
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("request %d failed: %v", i, err)
		}
	}

	created, conflicted := 0, 0
	for _, s := range statuses {
		switch s {
		case http.StatusCreated:
			created++
		case http.StatusConflict:
			conflicted++
		}
	}
	if created != 1 || conflicted != 1 {
		t.Fatalf("expected exactly one 201 and one 409, got statuses=%v", statuses)
	}
}

func TestTransactions_CreateBadConditionToCustomerRequiresConfirmation(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	stockItem := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(map[string]any{"condition": "BAD"}))
	customer := createCustomer(t, adminToken, map[string]any{"name": "Budi Santoso"})

	status, resp := doRequest(t, http.MethodPost, "/api/transactions", sellTransactionBody(customer.ID, []map[string]any{
		{"stock_item_id": stockItem.ID, "price_total": 100000},
	}), adminToken)
	if status != http.StatusConflict {
		t.Fatalf("without confirmed: expected 409, got %d (resp=%+v)", status, resp)
	}

	status, resp = doRequest(t, http.MethodPost, "/api/transactions", sellTransactionBody(customer.ID, []map[string]any{
		{"stock_item_id": stockItem.ID, "price_total": 100000, "confirmed": true},
	}), adminToken)
	if status != http.StatusCreated {
		t.Fatalf("with confirmed=true: expected 201, got %d (resp=%+v)", status, resp)
	}
}

func TestTransactions_CreateBadConditionToSupplierNoConfirmationNeeded(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	stockItem := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(map[string]any{"condition": "BAD"}))
	supplier := createSupplier(t, adminToken, map[string]any{"name": "Toko Emas Jaya"})

	status, resp := doRequest(t, http.MethodPost, "/api/transactions", map[string]any{
		"type":           "SELL_SUPPLIER",
		"supplier_id":    supplier.ID,
		"payment_method": "CASH",
		"items":          []map[string]any{{"stock_item_id": stockItem.ID, "price_total": 100000}},
	}, adminToken)
	if status != http.StatusCreated {
		t.Fatalf("expected 201 (no confirmation needed for SELL_SUPPLIER), got %d (resp=%+v)", status, resp)
	}
}

func TestTransactions_CreateInvalidPaymentMethod(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	stockItem := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(nil))
	customer := createCustomer(t, adminToken, map[string]any{"name": "Budi Santoso"})

	status, resp := doRequest(t, http.MethodPost, "/api/transactions", map[string]any{
		"type":           "SELL",
		"customer_id":    customer.ID,
		"payment_method": "BITCOIN",
		"items":          []map[string]any{{"stock_item_id": stockItem.ID, "price_total": 100000}},
	}, adminToken)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (resp=%+v)", status, resp)
	}
}

func TestTransactions_CreateAcceptsNewPaymentMethods(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	customer := createCustomer(t, adminToken, map[string]any{"name": "Budi Santoso"})

	for i, method := range []string{"DEBIT", "KREDIT", "GOPAY", "OVO", "DANA", "SHOPEEPAY"} {
		stockItem := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(map[string]any{
			"serial_number": fmt.Sprintf("SN-PM-%d", i),
		}))

		status, resp := doRequest(t, http.MethodPost, "/api/transactions", map[string]any{
			"type":           "SELL",
			"customer_id":    customer.ID,
			"payment_method": method,
			"items":          []map[string]any{{"stock_item_id": stockItem.ID, "price_total": 100000}},
		}, adminToken)
		if status != http.StatusCreated {
			t.Fatalf("payment_method=%s: expected 201, got %d (resp=%+v)", method, status, resp)
		}
		var created transactionDTO
		decodeData(t, resp, &created)
		if created.PaymentMethod != method {
			t.Fatalf("payment_method=%s: expected echoed back, got %q", method, created.PaymentMethod)
		}
	}
}

func TestTransactions_CreateEmptyItems(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	customer := createCustomer(t, adminToken, map[string]any{"name": "Budi Santoso"})

	status, resp := doRequest(t, http.MethodPost, "/api/transactions", sellTransactionBody(customer.ID, []map[string]any{}), adminToken)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (resp=%+v)", status, resp)
	}
}

func TestTransactions_ResponseExcludesCOGS(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	stockItem := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(nil))
	customer := createCustomer(t, adminToken, map[string]any{"name": "Budi Santoso"})

	status, resp := doRequest(t, http.MethodPost, "/api/transactions", sellTransactionBody(customer.ID, []map[string]any{
		{"stock_item_id": stockItem.ID, "price_total": 100000},
	}), adminToken)
	if status != http.StatusCreated {
		t.Fatalf("expected 201, got %d (resp=%+v)", status, resp)
	}
	if strings.Contains(strings.ToLower(string(resp.Data)), "cogs") {
		t.Fatalf("response must never include cogs, got raw data: %s", resp.Data)
	}
}

// --- BE-801: buyback (type=BUY) ---

func buyTransactionBody(customerID string, items []map[string]any) map[string]any {
	return map[string]any{
		"type":           "BUY",
		"customer_id":    customerID,
		"payment_method": "CASH",
		"items":          items,
	}
}

func TestTransactions_CreateBuyFromCustomer(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken) // weight_gram = 10
	customer := createCustomer(t, adminToken, map[string]any{"name": "Budi Santoso"})

	status, resp := doRequest(t, http.MethodPost, "/api/transactions", buyTransactionBody(customer.ID, []map[string]any{
		{"product_id": product.ID, "serial_number": "BUY-SN-0001", "condition": "GOOD", "price_total": 900000},
	}), adminToken)
	if status != http.StatusCreated {
		t.Fatalf("expected 201, got %d (resp=%+v)", status, resp)
	}
	var tx transactionDTO
	decodeData(t, resp, &tx)

	if tx.Type != "BUY" {
		t.Fatalf("expected type BUY, got %q", tx.Type)
	}
	if len(tx.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(tx.Items))
	}
	item := tx.Items[0]
	if item.PricePerGram != 90000 {
		t.Fatalf("expected price_per_gram=90000, got %v", item.PricePerGram)
	}
	if item.StockItemID == "" {
		t.Fatalf("expected stock_item_id populated, got %+v", item)
	}
	assertValidGeneratedBarcode(t, item.Barcode)

	status, resp = doRequest(t, http.MethodGet, "/api/stock-items/"+item.StockItemID, nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("get created stock item: expected 200, got %d (resp=%+v)", status, resp)
	}
	var stockItem stockItemDTO
	decodeData(t, resp, &stockItem)
	if stockItem.Status != "AVAILABLE" {
		t.Fatalf("expected new unit status AVAILABLE, got %q", stockItem.Status)
	}
	if stockItem.SerialNumber != "BUY-SN-0001" || stockItem.Condition != "GOOD" {
		t.Fatalf("expected serial/condition to match input, got %+v", stockItem)
	}
	if stockItem.PurchasePrice != 900000 {
		t.Fatalf("expected purchase_price=900000 (=price_total), got %v", stockItem.PurchasePrice)
	}

	if strings.Contains(strings.ToLower(string(resp.Data)), "cogs") {
		t.Fatalf("stock item response must never include cogs, got raw data: %s", resp.Data)
	}
}

func TestTransactions_BuyWithProductionYear(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	customer := createCustomer(t, adminToken, map[string]any{"name": "Budi Santoso"})

	status, resp := doRequest(t, http.MethodPost, "/api/transactions", buyTransactionBody(customer.ID, []map[string]any{
		{"product_id": product.ID, "serial_number": "BUY-SN-YEAR", "condition": "GOOD", "price_total": 900000, "production_year": 2023},
	}), adminToken)
	if status != http.StatusCreated {
		t.Fatalf("expected 201, got %d (resp=%+v)", status, resp)
	}
	var tx transactionDTO
	decodeData(t, resp, &tx)

	status, resp = doRequest(t, http.MethodGet, "/api/stock-items/"+tx.Items[0].StockItemID, nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("get created stock item: expected 200, got %d (resp=%+v)", status, resp)
	}
	var stockItem stockItemDTO
	decodeData(t, resp, &stockItem)
	if stockItem.ProductionYear == nil || *stockItem.ProductionYear != 2023 {
		t.Fatalf("expected production_year 2023 on new unit, got %v", stockItem.ProductionYear)
	}
}

func TestTransactions_BuyMultipleItemsSameProductGetsDistinctBarcodes(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	customer := createCustomer(t, adminToken, map[string]any{"name": "Budi Santoso"})

	status, resp := doRequest(t, http.MethodPost, "/api/transactions", buyTransactionBody(customer.ID, []map[string]any{
		{"product_id": product.ID, "serial_number": "BUY-SN-A", "condition": "GOOD", "price_total": 900000},
		{"product_id": product.ID, "serial_number": "BUY-SN-B", "condition": "GOOD", "price_total": 950000},
	}), adminToken)
	if status != http.StatusCreated {
		t.Fatalf("expected 201, got %d (resp=%+v)", status, resp)
	}
	var tx transactionDTO
	decodeData(t, resp, &tx)
	if len(tx.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(tx.Items))
	}
	assertValidGeneratedBarcode(t, tx.Items[0].Barcode)
	assertValidGeneratedBarcode(t, tx.Items[1].Barcode)
	if tx.Items[0].Barcode == tx.Items[1].Barcode {
		t.Fatalf("expected distinct barcodes for the 2 items, both were %q", tx.Items[0].Barcode)
	}
}

func TestTransactions_BuyIncreasesProductStockCount(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	customer := createCustomer(t, adminToken, map[string]any{"name": "Budi Santoso"})

	status, resp := doRequest(t, http.MethodGet, "/api/products/"+product.ID+"/stock-items", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("list before: expected 200, got %d (resp=%+v)", status, resp)
	}
	var before stockItemListDTO
	decodeData(t, resp, &before)

	status, resp = doRequest(t, http.MethodPost, "/api/transactions", buyTransactionBody(customer.ID, []map[string]any{
		{"product_id": product.ID, "serial_number": "BUY-SN-COUNT-1", "condition": "GOOD", "price_total": 900000},
		{"product_id": product.ID, "serial_number": "BUY-SN-COUNT-2", "condition": "GOOD", "price_total": 900000},
	}), adminToken)
	if status != http.StatusCreated {
		t.Fatalf("expected 201, got %d (resp=%+v)", status, resp)
	}

	status, resp = doRequest(t, http.MethodGet, "/api/products/"+product.ID+"/stock-items", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("list after: expected 200, got %d (resp=%+v)", status, resp)
	}
	var after stockItemListDTO
	decodeData(t, resp, &after)

	if after.Pagination.Total != before.Pagination.Total+2 {
		t.Fatalf("expected stock count to increase by 2, before=%d after=%d", before.Pagination.Total, after.Pagination.Total)
	}
}

func TestTransactions_BuyDuplicateSerialNumberInSameBatchRejected(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	customer := createCustomer(t, adminToken, map[string]any{"name": "Budi Santoso"})

	status, resp := doRequest(t, http.MethodPost, "/api/transactions", buyTransactionBody(customer.ID, []map[string]any{
		{"product_id": product.ID, "serial_number": "DUP-SN", "condition": "GOOD", "price_total": 900000},
		{"product_id": product.ID, "serial_number": "DUP-SN", "condition": "GOOD", "price_total": 950000},
	}), adminToken)
	if status != http.StatusConflict {
		t.Fatalf("expected 409, got %d (resp=%+v)", status, resp)
	}

	status, resp = doRequest(t, http.MethodGet, "/api/products/"+product.ID+"/stock-items", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("list: expected 200, got %d (resp=%+v)", status, resp)
	}
	var list stockItemListDTO
	decodeData(t, resp, &list)
	if list.Pagination.Total != 0 {
		t.Fatalf("expected nothing persisted after rejected batch, got total=%d", list.Pagination.Total)
	}
}

func TestTransactions_BuyDuplicateSerialNumberAgainstExistingUnitRejected(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	customer := createCustomer(t, adminToken, map[string]any{"name": "Budi Santoso"})
	createStockItemAPI(t, adminToken, product.ID, validStockItemBody(map[string]any{"serial_number": "EXISTING-SN"}))

	status, resp := doRequest(t, http.MethodPost, "/api/transactions", buyTransactionBody(customer.ID, []map[string]any{
		{"product_id": product.ID, "serial_number": "EXISTING-SN", "condition": "GOOD", "price_total": 900000},
	}), adminToken)
	if status != http.StatusConflict {
		t.Fatalf("expected 409, got %d (resp=%+v)", status, resp)
	}
}

func TestTransactions_BuyMissingSerialNumber(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	customer := createCustomer(t, adminToken, map[string]any{"name": "Budi Santoso"})

	status, resp := doRequest(t, http.MethodPost, "/api/transactions", buyTransactionBody(customer.ID, []map[string]any{
		{"product_id": product.ID, "serial_number": "", "condition": "GOOD", "price_total": 900000},
	}), adminToken)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d (resp=%+v)", status, resp)
	}
}

func TestTransactions_BuyInvalidCondition(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	customer := createCustomer(t, adminToken, map[string]any{"name": "Budi Santoso"})

	status, resp := doRequest(t, http.MethodPost, "/api/transactions", buyTransactionBody(customer.ID, []map[string]any{
		{"product_id": product.ID, "serial_number": "BUY-SN-X", "condition": "OK", "price_total": 900000},
	}), adminToken)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d (resp=%+v)", status, resp)
	}
}

func TestTransactions_BuyInvalidPriceTotal(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	customer := createCustomer(t, adminToken, map[string]any{"name": "Budi Santoso"})

	status, resp := doRequest(t, http.MethodPost, "/api/transactions", buyTransactionBody(customer.ID, []map[string]any{
		{"product_id": product.ID, "serial_number": "BUY-SN-X", "condition": "GOOD", "price_total": 0},
	}), adminToken)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (resp=%+v)", status, resp)
	}
}

func TestTransactions_BuyMissingCustomerID(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)

	status, resp := doRequest(t, http.MethodPost, "/api/transactions", map[string]any{
		"type":           "BUY",
		"payment_method": "CASH",
		"items": []map[string]any{
			{"product_id": product.ID, "serial_number": "BUY-SN-X", "condition": "GOOD", "price_total": 900000},
		},
	}, adminToken)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (resp=%+v)", status, resp)
	}
}

func TestTransactions_BuyProductNotFound(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	customer := createCustomer(t, adminToken, map[string]any{"name": "Budi Santoso"})

	status, resp := doRequest(t, http.MethodPost, "/api/transactions", buyTransactionBody(customer.ID, []map[string]any{
		{"product_id": nonexistentUUID, "serial_number": "BUY-SN-X", "condition": "GOOD", "price_total": 900000},
	}), adminToken)
	if status != http.StatusNotFound {
		t.Fatalf("expected 404, got %d (resp=%+v)", status, resp)
	}
}

func TestTransactions_BuyProductArchivedRejected(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	customer := createCustomer(t, adminToken, map[string]any{"name": "Budi Santoso"})

	status, resp := doRequest(t, http.MethodDelete, "/api/products/"+product.ID, nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("archive product: expected 200, got %d (resp=%+v)", status, resp)
	}

	status, resp = doRequest(t, http.MethodPost, "/api/transactions", buyTransactionBody(customer.ID, []map[string]any{
		{"product_id": product.ID, "serial_number": "BUY-SN-X", "condition": "GOOD", "price_total": 900000},
	}), adminToken)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (resp=%+v)", status, resp)
	}
}

// --- BE-602: customer transaction history + transaction detail ---

func TestTransactions_CustomerHistoryRequiresAuth(t *testing.T) {
	resetDB(t)

	status, _ := doRequest(t, http.MethodGet, "/api/customers/"+nonexistentUUID+"/transactions", nil, "")
	if status != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", status)
	}
}

func TestTransactions_CustomerHistoryCombinesSellAndBuy(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	customer := createCustomer(t, adminToken, map[string]any{"name": "Budi Santoso"})

	buyStatus, buyResp := doRequest(t, http.MethodPost, "/api/transactions", buyTransactionBody(customer.ID, []map[string]any{
		{"product_id": product.ID, "serial_number": "HIST-BUY-1", "condition": "GOOD", "price_total": 900000},
	}), adminToken)
	if buyStatus != http.StatusCreated {
		t.Fatalf("buy: expected 201, got %d (resp=%+v)", buyStatus, buyResp)
	}
	var buyTx transactionDTO
	decodeData(t, buyResp, &buyTx)

	sellItem := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(map[string]any{"serial_number": "HIST-SELL-1"}))
	sellStatus, sellResp := doRequest(t, http.MethodPost, "/api/transactions", sellTransactionBody(customer.ID, []map[string]any{
		{"stock_item_id": sellItem.ID, "price_total": 1200000},
	}), adminToken)
	if sellStatus != http.StatusCreated {
		t.Fatalf("sell: expected 201, got %d (resp=%+v)", sellStatus, sellResp)
	}
	var sellTx transactionDTO
	decodeData(t, sellResp, &sellTx)

	status, resp := doRequest(t, http.MethodGet, "/api/customers/"+customer.ID+"/transactions", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (resp=%+v)", status, resp)
	}
	var list transactionListDTO
	decodeData(t, resp, &list)
	if list.Pagination.Total != 2 {
		t.Fatalf("expected total=2, got %d", list.Pagination.Total)
	}
	foundBuy, foundSell := false, false
	for _, tx := range list.Items {
		if tx.ID == buyTx.ID && tx.Type == "BUY" {
			foundBuy = true
		}
		if tx.ID == sellTx.ID && tx.Type == "SELL" {
			foundSell = true
		}
	}
	if !foundBuy || !foundSell {
		t.Fatalf("expected both BUY and SELL in history, got %+v", list.Items)
	}
}

func TestTransactions_CustomerHistoryExcludesOtherCustomers(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	customerA := createCustomer(t, adminToken, map[string]any{"name": "Budi Santoso"})
	customerB := createCustomer(t, adminToken, map[string]any{"name": "Siti Aminah"})

	status, resp := doRequest(t, http.MethodPost, "/api/transactions", buyTransactionBody(customerB.ID, []map[string]any{
		{"product_id": product.ID, "serial_number": "OTHER-CUST-1", "condition": "GOOD", "price_total": 900000},
	}), adminToken)
	if status != http.StatusCreated {
		t.Fatalf("expected 201, got %d (resp=%+v)", status, resp)
	}

	status, resp = doRequest(t, http.MethodGet, "/api/customers/"+customerA.ID+"/transactions", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (resp=%+v)", status, resp)
	}
	var list transactionListDTO
	decodeData(t, resp, &list)
	if list.Pagination.Total != 0 {
		t.Fatalf("expected customer A history empty, got total=%d", list.Pagination.Total)
	}
}

func TestTransactions_CustomerHistoryOrderedNewestFirst(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	customer := createCustomer(t, adminToken, map[string]any{"name": "Budi Santoso"})

	var codes []string
	for i := 0; i < 3; i++ {
		status, resp := doRequest(t, http.MethodPost, "/api/transactions", buyTransactionBody(customer.ID, []map[string]any{
			{"product_id": product.ID, "serial_number": fmt.Sprintf("ORDER-SN-%d", i), "condition": "GOOD", "price_total": 900000},
		}), adminToken)
		if status != http.StatusCreated {
			t.Fatalf("create %d: expected 201, got %d (resp=%+v)", i, status, resp)
		}
		var tx transactionDTO
		decodeData(t, resp, &tx)
		codes = append(codes, tx.TransactionCode)
	}

	status, resp := doRequest(t, http.MethodGet, "/api/customers/"+customer.ID+"/transactions", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (resp=%+v)", status, resp)
	}
	var list transactionListDTO
	decodeData(t, resp, &list)
	if len(list.Items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(list.Items))
	}
	// Newest first: the last transaction created (codes[2]) must appear first.
	if list.Items[0].TransactionCode != codes[2] || list.Items[2].TransactionCode != codes[0] {
		t.Fatalf("expected newest-first order %v, got %+v", codes, list.Items)
	}
}

func TestTransactions_CustomerHistoryPagination(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	customer := createCustomer(t, adminToken, map[string]any{"name": "Budi Santoso"})

	for i := 0; i < 3; i++ {
		status, resp := doRequest(t, http.MethodPost, "/api/transactions", buyTransactionBody(customer.ID, []map[string]any{
			{"product_id": product.ID, "serial_number": fmt.Sprintf("PAGE-SN-%d", i), "condition": "GOOD", "price_total": 900000},
		}), adminToken)
		if status != http.StatusCreated {
			t.Fatalf("create %d: expected 201, got %d (resp=%+v)", i, status, resp)
		}
	}

	status, resp := doRequest(t, http.MethodGet, "/api/customers/"+customer.ID+"/transactions?limit=2&page=1", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("page 1: expected 200, got %d (resp=%+v)", status, resp)
	}
	var page1 transactionListDTO
	decodeData(t, resp, &page1)
	if len(page1.Items) != 2 || page1.Pagination.Total != 3 || page1.Pagination.TotalPages != 2 {
		t.Fatalf("page 1: expected 2 items total=3 total_pages=2, got %d items %+v", len(page1.Items), page1.Pagination)
	}

	status, resp = doRequest(t, http.MethodGet, "/api/customers/"+customer.ID+"/transactions?limit=2&page=2", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("page 2: expected 200, got %d (resp=%+v)", status, resp)
	}
	var page2 transactionListDTO
	decodeData(t, resp, &page2)
	if len(page2.Items) != 1 {
		t.Fatalf("page 2: expected 1 item, got %d", len(page2.Items))
	}
}

func TestTransactions_CustomerHistoryCustomerNotFound(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	status, resp := doRequest(t, http.MethodGet, "/api/customers/"+nonexistentUUID+"/transactions", nil, adminToken)
	if status != http.StatusNotFound {
		t.Fatalf("expected 404, got %d (resp=%+v)", status, resp)
	}
}

// --- GET /api/transactions (BE-1401): per-transaction list backing the
// sales/buyback screens, as opposed to /reports/transactions which stays
// aggregate-only ---

func TestTransactions_ListRequiresAuth(t *testing.T) {
	resetDB(t)

	status, _ := doRequest(t, http.MethodGet, "/api/transactions", nil, "")
	if status != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", status)
	}
}

func TestTransactions_ListAllowedForKasir(t *testing.T) {
	resetDB(t)
	kasir := seedUser(t, "KASIR", true)
	kasirToken := login(t, kasir.Email, kasir.Password)

	status, resp := doRequest(t, http.MethodGet, "/api/transactions", nil, kasirToken)
	if status != http.StatusOK {
		t.Fatalf("expected 200 for KASIR (same tier as GET /transactions/{id}), got %d (resp=%+v)", status, resp)
	}
}

func TestTransactions_ListCombinesAllTypesAcrossCustomersAndSuppliers(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	customer := createCustomer(t, adminToken, map[string]any{"name": "Budi Santoso"})
	supplier := createSupplier(t, adminToken, map[string]any{"name": "Toko Emas Jaya"})

	buyStatus, buyResp := doRequest(t, http.MethodPost, "/api/transactions", buyTransactionBody(customer.ID, []map[string]any{
		{"product_id": product.ID, "serial_number": "LIST-BUY-1", "condition": "GOOD", "price_total": 900000},
	}), adminToken)
	if buyStatus != http.StatusCreated {
		t.Fatalf("buy: expected 201, got %d (resp=%+v)", buyStatus, buyResp)
	}
	var buyTx transactionDTO
	decodeData(t, buyResp, &buyTx)

	sellItem := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(map[string]any{"serial_number": "LIST-SELL-1"}))
	sellStatus, sellResp := doRequest(t, http.MethodPost, "/api/transactions", sellTransactionBody(customer.ID, []map[string]any{
		{"stock_item_id": sellItem.ID, "price_total": 1200000},
	}), adminToken)
	if sellStatus != http.StatusCreated {
		t.Fatalf("sell: expected 201, got %d (resp=%+v)", sellStatus, sellResp)
	}
	var sellTx transactionDTO
	decodeData(t, sellResp, &sellTx)

	supplierItem := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(map[string]any{"serial_number": "LIST-SUP-1"}))
	supStatus, supResp := doRequest(t, http.MethodPost, "/api/transactions", map[string]any{
		"type":           "SELL_SUPPLIER",
		"supplier_id":    supplier.ID,
		"payment_method": "CASH",
		"items":          []map[string]any{{"stock_item_id": supplierItem.ID, "price_total": 800000}},
	}, adminToken)
	if supStatus != http.StatusCreated {
		t.Fatalf("sell_supplier: expected 201, got %d (resp=%+v)", supStatus, supResp)
	}
	var supTx transactionDTO
	decodeData(t, supResp, &supTx)

	status, resp := doRequest(t, http.MethodGet, "/api/transactions", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (resp=%+v)", status, resp)
	}
	var list generalTransactionListDTO
	decodeData(t, resp, &list)
	if list.Pagination.Total != 3 {
		t.Fatalf("expected total=3, got %d", list.Pagination.Total)
	}
	foundBuy, foundSell, foundSup := false, false, false
	for _, tx := range list.Items {
		switch {
		case tx.ID == buyTx.ID && tx.Type == "BUY":
			foundBuy = true
			if tx.Customer == nil || tx.Customer.Name != "Budi Santoso" {
				t.Fatalf("expected BUY row to have customer ref, got %+v", tx.Customer)
			}
			if tx.Supplier != nil {
				t.Fatalf("expected BUY row to have no supplier ref, got %+v", tx.Supplier)
			}
		case tx.ID == sellTx.ID && tx.Type == "SELL":
			foundSell = true
			if tx.Customer == nil || tx.Customer.Name != "Budi Santoso" {
				t.Fatalf("expected SELL row to have customer ref, got %+v", tx.Customer)
			}
		case tx.ID == supTx.ID && tx.Type == "SELL_SUPPLIER":
			foundSup = true
			if tx.Supplier == nil || tx.Supplier.Name != "Toko Emas Jaya" {
				t.Fatalf("expected SELL_SUPPLIER row to have supplier ref, got %+v", tx.Supplier)
			}
			if tx.Customer != nil {
				t.Fatalf("expected SELL_SUPPLIER row to have no customer ref, got %+v", tx.Customer)
			}
		}
	}
	if !foundBuy || !foundSell || !foundSup {
		t.Fatalf("expected BUY, SELL and SELL_SUPPLIER all present, got %+v", list.Items)
	}
}

func TestTransactions_ListFilterByType(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
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
		{"product_id": product.ID, "serial_number": "LIST-FILTER-BUY", "condition": "GOOD", "price_total": 900000},
	}), adminToken)
	if status != http.StatusCreated {
		t.Fatalf("buy: expected 201, got %d (resp=%+v)", status, resp)
	}

	status, resp = doRequest(t, http.MethodGet, "/api/transactions?type=SELL", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (resp=%+v)", status, resp)
	}
	var list generalTransactionListDTO
	decodeData(t, resp, &list)
	if list.Pagination.Total != 1 || len(list.Items) != 1 || list.Items[0].Type != "SELL" {
		t.Fatalf("expected only 1 SELL transaction, got %+v", list)
	}
}

func TestTransactions_ListInvalidType(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	status, resp := doRequest(t, http.MethodGet, "/api/transactions?type=INVALID", nil, adminToken)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (resp=%+v)", status, resp)
	}
}

func TestTransactions_ListFilterByDateRange(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	customer := createCustomer(t, adminToken, map[string]any{"name": "Budi Santoso"})

	inRangeItem := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(map[string]any{"serial_number": "LIST-DATE-IN"}))
	status, resp := doRequest(t, http.MethodPost, "/api/transactions", sellTransactionBody(customer.ID, []map[string]any{
		{"stock_item_id": inRangeItem.ID, "price_total": 1000000},
	}), adminToken)
	if status != http.StatusCreated {
		t.Fatalf("in-range sell: expected 201, got %d (resp=%+v)", status, resp)
	}
	var inRangeTx transactionDTO
	decodeData(t, resp, &inRangeTx)
	setTransactionCreatedAt(t, inRangeTx.ID, time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC))

	outOfRangeItem := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(map[string]any{"serial_number": "LIST-DATE-OUT"}))
	status, resp = doRequest(t, http.MethodPost, "/api/transactions", sellTransactionBody(customer.ID, []map[string]any{
		{"stock_item_id": outOfRangeItem.ID, "price_total": 2000000},
	}), adminToken)
	if status != http.StatusCreated {
		t.Fatalf("out-of-range sell: expected 201, got %d (resp=%+v)", status, resp)
	}
	var outOfRangeTx transactionDTO
	decodeData(t, resp, &outOfRangeTx)
	setTransactionCreatedAt(t, outOfRangeTx.ID, time.Date(2026, 8, 15, 10, 0, 0, 0, time.UTC))

	status, resp = doRequest(t, http.MethodGet, "/api/transactions?from=2026-07-01&to=2026-07-31", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (resp=%+v)", status, resp)
	}
	var list generalTransactionListDTO
	decodeData(t, resp, &list)
	if list.Pagination.Total != 1 || len(list.Items) != 1 || list.Items[0].ID != inRangeTx.ID {
		t.Fatalf("expected only the July transaction, got %+v", list)
	}
}

func TestTransactions_ListBadDateFormat(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	status, resp := doRequest(t, http.MethodGet, "/api/transactions?from=01-07-2026", nil, adminToken)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (resp=%+v)", status, resp)
	}
}

func TestTransactions_ListOrderedNewestFirst(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	customer := createCustomer(t, adminToken, map[string]any{"name": "Budi Santoso"})

	var codes []string
	for i := 0; i < 3; i++ {
		status, resp := doRequest(t, http.MethodPost, "/api/transactions", buyTransactionBody(customer.ID, []map[string]any{
			{"product_id": product.ID, "serial_number": fmt.Sprintf("LIST-ORDER-SN-%d", i), "condition": "GOOD", "price_total": 900000},
		}), adminToken)
		if status != http.StatusCreated {
			t.Fatalf("create %d: expected 201, got %d (resp=%+v)", i, status, resp)
		}
		var tx transactionDTO
		decodeData(t, resp, &tx)
		codes = append(codes, tx.TransactionCode)
	}

	status, resp := doRequest(t, http.MethodGet, "/api/transactions", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (resp=%+v)", status, resp)
	}
	var list generalTransactionListDTO
	decodeData(t, resp, &list)
	if len(list.Items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(list.Items))
	}
	if list.Items[0].TransactionCode != codes[2] || list.Items[2].TransactionCode != codes[0] {
		t.Fatalf("expected newest-first order %v, got %+v", codes, list.Items)
	}
}

func TestTransactions_ListPagination(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	customer := createCustomer(t, adminToken, map[string]any{"name": "Budi Santoso"})

	for i := 0; i < 3; i++ {
		status, resp := doRequest(t, http.MethodPost, "/api/transactions", buyTransactionBody(customer.ID, []map[string]any{
			{"product_id": product.ID, "serial_number": fmt.Sprintf("LIST-PAGE-SN-%d", i), "condition": "GOOD", "price_total": 900000},
		}), adminToken)
		if status != http.StatusCreated {
			t.Fatalf("create %d: expected 201, got %d (resp=%+v)", i, status, resp)
		}
	}

	status, resp := doRequest(t, http.MethodGet, "/api/transactions?limit=2&page=1", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("page 1: expected 200, got %d (resp=%+v)", status, resp)
	}
	var page1 generalTransactionListDTO
	decodeData(t, resp, &page1)
	if len(page1.Items) != 2 || page1.Pagination.Total != 3 || page1.Pagination.TotalPages != 2 {
		t.Fatalf("page 1: expected 2 items total=3 total_pages=2, got %d items %+v", len(page1.Items), page1.Pagination)
	}

	status, resp = doRequest(t, http.MethodGet, "/api/transactions?limit=2&page=2", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("page 2: expected 200, got %d (resp=%+v)", status, resp)
	}
	var page2 generalTransactionListDTO
	decodeData(t, resp, &page2)
	if len(page2.Items) != 1 {
		t.Fatalf("page 2: expected 1 item, got %d", len(page2.Items))
	}
}

func TestTransactions_GetDetailReturnsFullItems(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	customer := createCustomer(t, adminToken, map[string]any{"name": "Budi Santoso"})
	stockItem := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(nil))

	status, resp := doRequest(t, http.MethodPost, "/api/transactions", sellTransactionBody(customer.ID, []map[string]any{
		{"stock_item_id": stockItem.ID, "price_total": 1500000},
	}), adminToken)
	if status != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d (resp=%+v)", status, resp)
	}
	var created transactionDTO
	decodeData(t, resp, &created)

	status, resp = doRequest(t, http.MethodGet, "/api/transactions/"+created.ID, nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("get: expected 200, got %d (resp=%+v)", status, resp)
	}
	var fetched transactionDTO
	decodeData(t, resp, &fetched)
	if fetched.TransactionCode != created.TransactionCode || len(fetched.Items) != 1 {
		t.Fatalf("expected full detail matching create response, got %+v", fetched)
	}
	if fetched.Items[0].StockItemID != stockItem.ID || fetched.Items[0].Barcode != stockItem.Barcode {
		t.Fatalf("expected item stock_item_id/barcode populated, got %+v", fetched.Items[0])
	}
	if fetched.Items[0].SerialNumber != stockItem.SerialNumber {
		t.Fatalf("expected item serial_number to match stock item, got %+v", fetched.Items[0])
	}
	if strings.Contains(strings.ToLower(string(resp.Data)), "cogs") {
		t.Fatalf("detail response must never include cogs, got raw data: %s", resp.Data)
	}
}

func TestTransactions_GetDetailWorksForBuyToo(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	customer := createCustomer(t, adminToken, map[string]any{"name": "Budi Santoso"})

	status, resp := doRequest(t, http.MethodPost, "/api/transactions", buyTransactionBody(customer.ID, []map[string]any{
		{"product_id": product.ID, "serial_number": "DETAIL-BUY-SN", "condition": "GOOD", "price_total": 900000},
	}), adminToken)
	if status != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d (resp=%+v)", status, resp)
	}
	var created transactionDTO
	decodeData(t, resp, &created)

	status, resp = doRequest(t, http.MethodGet, "/api/transactions/"+created.ID, nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("get: expected 200, got %d (resp=%+v)", status, resp)
	}
	var fetched transactionDTO
	decodeData(t, resp, &fetched)
	if fetched.Type != "BUY" || len(fetched.Items) != 1 {
		t.Fatalf("expected BUY detail with 1 item, got %+v", fetched)
	}
	if fetched.Items[0].SerialNumber != "DETAIL-BUY-SN" {
		t.Fatalf("expected item serial_number to match the buy input, got %+v", fetched.Items[0])
	}
}

func TestTransactions_PaymentRefRoundTripsThroughCreateGetAndHistory(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	customer := createCustomer(t, adminToken, map[string]any{"name": "Budi Santoso"})
	stockItem := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(nil))

	status, resp := doRequest(t, http.MethodPost, "/api/transactions", map[string]any{
		"type":           "SELL",
		"customer_id":    customer.ID,
		"payment_method": "TRANSFER",
		"payment_ref":    "BCA - 88812345",
		"items":          []map[string]any{{"stock_item_id": stockItem.ID, "price_total": 1500000}},
	}, adminToken)
	if status != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d (resp=%+v)", status, resp)
	}
	var created transactionDTO
	decodeData(t, resp, &created)
	if created.PaymentRef != "BCA - 88812345" {
		t.Fatalf("create: expected payment_ref echoed back, got %q", created.PaymentRef)
	}

	status, resp = doRequest(t, http.MethodGet, "/api/transactions/"+created.ID, nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("get: expected 200, got %d (resp=%+v)", status, resp)
	}
	var fetched transactionDTO
	decodeData(t, resp, &fetched)
	if fetched.PaymentRef != "BCA - 88812345" {
		t.Fatalf("get: expected payment_ref persisted, got %q", fetched.PaymentRef)
	}

	status, resp = doRequest(t, http.MethodGet, "/api/customers/"+customer.ID+"/transactions", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("history: expected 200, got %d (resp=%+v)", status, resp)
	}
	var history transactionListDTO
	decodeData(t, resp, &history)
	if len(history.Items) != 1 || history.Items[0].PaymentRef != "BCA - 88812345" {
		t.Fatalf("history: expected payment_ref surfaced in customer history, got %+v", history.Items)
	}
}

func TestTransactions_PaymentRefEmptyForCash(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	customer := createCustomer(t, adminToken, map[string]any{"name": "Budi Santoso"})
	stockItem := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(nil))

	status, resp := doRequest(t, http.MethodPost, "/api/transactions", sellTransactionBody(customer.ID, []map[string]any{
		{"stock_item_id": stockItem.ID, "price_total": 1500000},
	}), adminToken)
	if status != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d (resp=%+v)", status, resp)
	}
	var created transactionDTO
	decodeData(t, resp, &created)
	if created.PaymentRef != "" {
		t.Fatalf("expected empty payment_ref when not supplied (CASH), got %q", created.PaymentRef)
	}
}

func TestTransactions_GetNotFound(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	status, resp := doRequest(t, http.MethodGet, "/api/transactions/"+nonexistentUUID, nil, adminToken)
	if status != http.StatusNotFound {
		t.Fatalf("expected 404, got %d (resp=%+v)", status, resp)
	}
}

func TestTransactions_GetInvalidIDFormat(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	status, resp := doRequest(t, http.MethodGet, "/api/transactions/1", nil, adminToken)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (resp=%+v)", status, resp)
	}
}

// --- cancel ---

func TestTransactions_CancelRequiresAuth(t *testing.T) {
	resetDB(t)

	status, _ := doRequest(t, http.MethodPost, "/api/transactions/"+nonexistentUUID+"/cancel", nil, "")
	if status != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", status)
	}
}

func TestTransactions_CancelForbiddenForKasir(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	stockItem := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(nil))
	customer := createCustomer(t, adminToken, map[string]any{"name": "Budi Santoso"})

	status, resp := doRequest(t, http.MethodPost, "/api/transactions", sellTransactionBody(customer.ID, []map[string]any{
		{"stock_item_id": stockItem.ID, "price_total": 1500000},
	}), adminToken)
	if status != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d (resp=%+v)", status, resp)
	}
	var tx transactionDTO
	decodeData(t, resp, &tx)

	kasir := seedUser(t, "KASIR", true)
	kasirToken := login(t, kasir.Email, kasir.Password)

	status, resp = doRequest(t, http.MethodPost, "/api/transactions/"+tx.ID+"/cancel", nil, kasirToken)
	if status != http.StatusForbidden {
		t.Fatalf("expected 403 for KASIR, got %d (resp=%+v)", status, resp)
	}
}

func TestTransactions_CancelNotFound(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	status, resp := doRequest(t, http.MethodPost, "/api/transactions/"+nonexistentUUID+"/cancel", nil, adminToken)
	if status != http.StatusNotFound {
		t.Fatalf("expected 404, got %d (resp=%+v)", status, resp)
	}
}

func TestTransactions_CancelSellRevertsStockToAvailableAndSellableAgain(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	stockItem := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(nil))
	customer := createCustomer(t, adminToken, map[string]any{"name": "Budi Santoso"})

	status, resp := doRequest(t, http.MethodPost, "/api/transactions", sellTransactionBody(customer.ID, []map[string]any{
		{"stock_item_id": stockItem.ID, "price_total": 1500000},
	}), adminToken)
	if status != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d (resp=%+v)", status, resp)
	}
	var tx transactionDTO
	decodeData(t, resp, &tx)

	status, resp = doRequest(t, http.MethodPost, "/api/transactions/"+tx.ID+"/cancel", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("cancel: expected 200, got %d (resp=%+v)", status, resp)
	}
	var cancelled transactionDTO
	decodeData(t, resp, &cancelled)
	if cancelled.Status != "CANCELLED" {
		t.Fatalf("expected status CANCELLED, got %q", cancelled.Status)
	}

	status, resp = doRequest(t, http.MethodGet, "/api/stock-items/"+stockItem.ID, nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("get stock item: expected 200, got %d (resp=%+v)", status, resp)
	}
	var fetched stockItemDTO
	decodeData(t, resp, &fetched)
	if fetched.Status != "AVAILABLE" {
		t.Fatalf("expected unit status AVAILABLE after cancel, got %q", fetched.Status)
	}

	// Sellable again, exactly as if it had never been sold.
	status, resp = doRequest(t, http.MethodPost, "/api/transactions", sellTransactionBody(customer.ID, []map[string]any{
		{"stock_item_id": stockItem.ID, "price_total": 1600000},
	}), adminToken)
	if status != http.StatusCreated {
		t.Fatalf("re-sell after cancel: expected 201, got %d (resp=%+v)", status, resp)
	}
}

func TestTransactions_CancelBuyVoidsCreatedStockItem(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	customer := createCustomer(t, adminToken, map[string]any{"name": "Budi Santoso"})

	status, resp := doRequest(t, http.MethodPost, "/api/transactions", buyTransactionBody(customer.ID, []map[string]any{
		{"product_id": product.ID, "serial_number": "CANCEL-BUY-SN", "condition": "GOOD", "price_total": 900000},
	}), adminToken)
	if status != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d (resp=%+v)", status, resp)
	}
	var tx transactionDTO
	decodeData(t, resp, &tx)
	stockItemID := tx.Items[0].StockItemID

	status, resp = doRequest(t, http.MethodPost, "/api/transactions/"+tx.ID+"/cancel", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("cancel: expected 200, got %d (resp=%+v)", status, resp)
	}
	var cancelled transactionDTO
	decodeData(t, resp, &cancelled)
	if cancelled.Status != "CANCELLED" {
		t.Fatalf("expected status CANCELLED, got %q", cancelled.Status)
	}

	status, resp = doRequest(t, http.MethodGet, "/api/stock-items/"+stockItemID, nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("get stock item: expected 200, got %d (resp=%+v)", status, resp)
	}
	var fetched stockItemDTO
	decodeData(t, resp, &fetched)
	if fetched.Status != "VOID" {
		t.Fatalf("expected unit status VOID after cancelling buy, got %q", fetched.Status)
	}

	// A VOID unit is never in stock — a serial number reused via a fresh buy
	// must be accepted, same as it would be for a SOLD unit.
	status, resp = doRequest(t, http.MethodPost, "/api/transactions", buyTransactionBody(customer.ID, []map[string]any{
		{"product_id": product.ID, "serial_number": "CANCEL-BUY-SN", "condition": "GOOD", "price_total": 950000},
	}), adminToken)
	if status != http.StatusCreated {
		t.Fatalf("re-buy same serial after void: expected 201, got %d (resp=%+v)", status, resp)
	}
}

func TestTransactions_CancelBuyAlreadyResoldRejected(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	customer := createCustomer(t, adminToken, map[string]any{"name": "Budi Santoso"})

	status, resp := doRequest(t, http.MethodPost, "/api/transactions", buyTransactionBody(customer.ID, []map[string]any{
		{"product_id": product.ID, "serial_number": "RESOLD-BUY-SN", "condition": "GOOD", "price_total": 900000},
	}), adminToken)
	if status != http.StatusCreated {
		t.Fatalf("buy: expected 201, got %d (resp=%+v)", status, resp)
	}
	var buyTx transactionDTO
	decodeData(t, resp, &buyTx)
	stockItemID := buyTx.Items[0].StockItemID

	status, resp = doRequest(t, http.MethodPost, "/api/transactions", sellTransactionBody(customer.ID, []map[string]any{
		{"stock_item_id": stockItemID, "price_total": 1200000},
	}), adminToken)
	if status != http.StatusCreated {
		t.Fatalf("sell: expected 201, got %d (resp=%+v)", status, resp)
	}

	status, resp = doRequest(t, http.MethodPost, "/api/transactions/"+buyTx.ID+"/cancel", nil, adminToken)
	if status != http.StatusConflict {
		t.Fatalf("cancel buy of already-resold unit: expected 409, got %d (resp=%+v)", status, resp)
	}
}

// TestTransactions_CancelSellArchivedUnitRevertsToAvailable covers cancel
// winning over an in-between archive: a unit sold, then archived, then
// un-cancelled (its SELL cancelled) must revert straight back to AVAILABLE
// — same as the never-archived case — because cancelling the sale is
// exactly what should bring dead stock back to life. Anyone wanting a unit
// permanently gone needs to cancel first, then archive, not the other way
// around.
func TestTransactions_CancelSellArchivedUnitRevertsToAvailable(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	stockItem := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(nil))
	customer := createCustomer(t, adminToken, map[string]any{"name": "Budi Santoso"})

	status, resp := doRequest(t, http.MethodPost, "/api/transactions", sellTransactionBody(customer.ID, []map[string]any{
		{"stock_item_id": stockItem.ID, "price_total": 1500000},
	}), adminToken)
	if status != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d (resp=%+v)", status, resp)
	}
	var tx transactionDTO
	decodeData(t, resp, &tx)

	status, resp = doRequest(t, http.MethodDelete, "/api/stock-items/"+stockItem.ID, nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("archive sold unit: expected 200, got %d (resp=%+v)", status, resp)
	}

	status, resp = doRequest(t, http.MethodPost, "/api/transactions/"+tx.ID+"/cancel", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("cancel sell of archived unit: expected 200, got %d (resp=%+v)", status, resp)
	}
	var cancelled transactionDTO
	decodeData(t, resp, &cancelled)
	if cancelled.Status != "CANCELLED" {
		t.Fatalf("expected status CANCELLED, got %q", cancelled.Status)
	}

	status, resp = doRequest(t, http.MethodGet, "/api/stock-items/"+stockItem.ID, nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("get stock item: expected 200, got %d (resp=%+v)", status, resp)
	}
	var fetched stockItemDTO
	decodeData(t, resp, &fetched)
	if fetched.Status != "AVAILABLE" {
		t.Fatalf("expected unit AVAILABLE again after cancel, got %q (archive should be overridden by cancel)", fetched.Status)
	}
}

// TestTransactions_CancelBuyArchivedUnitVoids mirrors the SELL case above
// for BUY: a unit archived after its originating buy falls through to VOID
// on cancel, same as a never-archived unit — only an actually-resold unit
// (SOLD) still blocks the cancel.
func TestTransactions_CancelBuyArchivedUnitVoids(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	customer := createCustomer(t, adminToken, map[string]any{"name": "Budi Santoso"})

	status, resp := doRequest(t, http.MethodPost, "/api/transactions", buyTransactionBody(customer.ID, []map[string]any{
		{"product_id": product.ID, "serial_number": "ARCHIVED-BUY-SN", "condition": "GOOD", "price_total": 900000},
	}), adminToken)
	if status != http.StatusCreated {
		t.Fatalf("buy: expected 201, got %d (resp=%+v)", status, resp)
	}
	var buyTx transactionDTO
	decodeData(t, resp, &buyTx)
	stockItemID := buyTx.Items[0].StockItemID

	status, resp = doRequest(t, http.MethodDelete, "/api/stock-items/"+stockItemID, nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("archive: expected 200, got %d (resp=%+v)", status, resp)
	}

	status, resp = doRequest(t, http.MethodPost, "/api/transactions/"+buyTx.ID+"/cancel", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("cancel buy of archived unit: expected 200, got %d (resp=%+v)", status, resp)
	}

	status, resp = doRequest(t, http.MethodGet, "/api/stock-items/"+stockItemID, nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("get stock item: expected 200, got %d (resp=%+v)", status, resp)
	}
	var fetched stockItemDTO
	decodeData(t, resp, &fetched)
	if fetched.Status != "VOID" {
		t.Fatalf("expected unit VOID after cancel (archive overridden by cancel), got %q", fetched.Status)
	}
}

func TestTransactions_CancelAlreadyCancelledConflict(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	stockItem := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(nil))
	customer := createCustomer(t, adminToken, map[string]any{"name": "Budi Santoso"})

	status, resp := doRequest(t, http.MethodPost, "/api/transactions", sellTransactionBody(customer.ID, []map[string]any{
		{"stock_item_id": stockItem.ID, "price_total": 1500000},
	}), adminToken)
	if status != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d (resp=%+v)", status, resp)
	}
	var tx transactionDTO
	decodeData(t, resp, &tx)

	status, resp = doRequest(t, http.MethodPost, "/api/transactions/"+tx.ID+"/cancel", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("first cancel: expected 200, got %d (resp=%+v)", status, resp)
	}

	status, resp = doRequest(t, http.MethodPost, "/api/transactions/"+tx.ID+"/cancel", nil, adminToken)
	if status != http.StatusConflict {
		t.Fatalf("second cancel: expected 409, got %d (resp=%+v)", status, resp)
	}
}

func TestTransactions_CancelAllowedForSuperAdmin(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	superAdmin := seedUser(t, "SUPER_ADMIN", true)
	superAdminToken := login(t, superAdmin.Email, superAdmin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	stockItem := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(nil))
	customer := createCustomer(t, adminToken, map[string]any{"name": "Budi Santoso"})

	status, resp := doRequest(t, http.MethodPost, "/api/transactions", sellTransactionBody(customer.ID, []map[string]any{
		{"stock_item_id": stockItem.ID, "price_total": 1500000},
	}), adminToken)
	if status != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d (resp=%+v)", status, resp)
	}
	var tx transactionDTO
	decodeData(t, resp, &tx)

	status, resp = doRequest(t, http.MethodPost, "/api/transactions/"+tx.ID+"/cancel", nil, superAdminToken)
	if status != http.StatusOK {
		t.Fatalf("cancel by SUPER_ADMIN: expected 200, got %d (resp=%+v)", status, resp)
	}
}
