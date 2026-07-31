package e2e

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

type transactionItemDTO struct {
	ID           string  `json:"id"`
	ProductName  string  `json:"product_name"`
	WeightGram   float64 `json:"weight_gram"`
	PricePerGram float64 `json:"price_per_gram"`
	PriceTotal   float64 `json:"price_total"`
}

type transactionDTO struct {
	ID              string               `json:"id"`
	TransactionCode string               `json:"transaction_code"`
	Type            string               `json:"type"`
	TotalAmount     float64              `json:"total_amount"`
	TotalWeight     float64              `json:"total_weight"`
	PaymentMethod   string               `json:"payment_method"`
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
