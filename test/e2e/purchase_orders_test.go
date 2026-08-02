package e2e

import (
	"context"
	"net/http"
	"testing"
	"time"
)

type purchaseOrderItemDTO struct {
	ID            string        `json:"id"`
	Product       productRefDTO `json:"product"`
	Quantity      int           `json:"quantity"`
	PurchasePrice float64       `json:"purchase_price"`
}

type receivedUnitDTO struct {
	StockItemID  string `json:"stock_item_id"`
	Barcode      string `json:"barcode"`
	ProductName  string `json:"product_name"`
	SerialNumber string `json:"serial_number"`
	Condition    string `json:"condition"`
}

type purchaseOrderDTO struct {
	ID            string                 `json:"id"`
	POCode        string                 `json:"po_code"`
	Supplier      productRefDTO          `json:"supplier"`
	TotalAmount   float64                `json:"total_amount"`
	Status        string                 `json:"status"`
	Notes         string                 `json:"notes"`
	Items         []purchaseOrderItemDTO `json:"items"`
	ReceivedUnits []receivedUnitDTO      `json:"received_units"`
	ReceivedAt    *string                `json:"received_at"`
}

type purchaseOrderListDTO struct {
	Items      []purchaseOrderDTO `json:"items"`
	Pagination paginationDTO      `json:"pagination"`
}

func createPurchaseOrder(t *testing.T, adminToken, supplierID string, items []map[string]any) purchaseOrderDTO {
	t.Helper()
	status, resp := doRequest(t, http.MethodPost, "/api/purchase-orders/", map[string]any{
		"supplier_id": supplierID,
		"items":       items,
	}, adminToken)
	if status != http.StatusCreated {
		t.Fatalf("create PO fixture: expected 201, got %d (resp=%+v)", status, resp)
	}
	var po purchaseOrderDTO
	decodeData(t, resp, &po)
	return po
}

// --- BE-901: create ---

func TestPurchaseOrders_CreateRequiresAuth(t *testing.T) {
	resetDB(t)

	status, _ := doRequest(t, http.MethodPost, "/api/purchase-orders/", nil, "")
	if status != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", status)
	}
}

func TestPurchaseOrders_CreateNonAdminForbidden(t *testing.T) {
	resetDB(t)
	kasir := seedUser(t, "KASIR", true)
	token := login(t, kasir.Email, kasir.Password)

	status, resp := doRequest(t, http.MethodPost, "/api/purchase-orders/", nil, token)
	if status != http.StatusForbidden {
		t.Fatalf("expected 403, got %d (resp=%+v)", status, resp)
	}
}

func TestPurchaseOrders_CreateSuccess(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	supplier := createSupplier(t, adminToken, map[string]any{"name": "Toko Emas Jaya"})

	status, resp := doRequest(t, http.MethodPost, "/api/purchase-orders/", map[string]any{
		"supplier_id": supplier.ID,
		"items": []map[string]any{
			{"product_id": product.ID, "quantity": 3, "purchase_price": 800000},
		},
	}, adminToken)
	if status != http.StatusCreated {
		t.Fatalf("expected 201, got %d (resp=%+v)", status, resp)
	}
	var po purchaseOrderDTO
	decodeData(t, resp, &po)

	today := time.Now().Format("20060102")
	expectedCode := "PO-" + today + "-0001"
	if po.POCode != expectedCode {
		t.Fatalf("expected po_code %q, got %q", expectedCode, po.POCode)
	}
	if po.Status != "BELUM_DITERIMA" {
		t.Fatalf("expected status BELUM_DITERIMA, got %q", po.Status)
	}
	if po.TotalAmount != 2400000 {
		t.Fatalf("expected total_amount=2400000 (3*800000), got %v", po.TotalAmount)
	}
	if len(po.Items) != 1 || po.Items[0].Quantity != 3 {
		t.Fatalf("expected 1 item quantity=3, got %+v", po.Items)
	}

	// No stock created yet.
	status, resp = doRequest(t, http.MethodGet, "/api/products/"+product.ID+"/stock-items", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("list stock: expected 200, got %d (resp=%+v)", status, resp)
	}
	var stockList stockItemListDTO
	decodeData(t, resp, &stockList)
	if stockList.Pagination.Total != 0 {
		t.Fatalf("expected no stock_items created by PO create, got total=%d", stockList.Pagination.Total)
	}
}

func TestPurchaseOrders_CreateCodeIncrementsSameDay(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	supplier := createSupplier(t, adminToken, map[string]any{"name": "Toko Emas Jaya"})

	items := []map[string]any{{"product_id": product.ID, "quantity": 1, "purchase_price": 800000}}
	first := createPurchaseOrder(t, adminToken, supplier.ID, items)
	second := createPurchaseOrder(t, adminToken, supplier.ID, items)

	today := time.Now().Format("20060102")
	if first.POCode != "PO-"+today+"-0001" {
		t.Fatalf("expected first code -0001, got %q", first.POCode)
	}
	if second.POCode != "PO-"+today+"-0002" {
		t.Fatalf("expected second code -0002, got %q", second.POCode)
	}
}

func TestPurchaseOrders_CreateMissingSupplierID(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)

	status, resp := doRequest(t, http.MethodPost, "/api/purchase-orders/", map[string]any{
		"items": []map[string]any{{"product_id": product.ID, "quantity": 1, "purchase_price": 800000}},
	}, adminToken)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (resp=%+v)", status, resp)
	}
}

func TestPurchaseOrders_CreateEmptyItems(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	supplier := createSupplier(t, adminToken, map[string]any{"name": "Toko Emas Jaya"})

	status, resp := doRequest(t, http.MethodPost, "/api/purchase-orders/", map[string]any{
		"supplier_id": supplier.ID,
		"items":       []map[string]any{},
	}, adminToken)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (resp=%+v)", status, resp)
	}
}

func TestPurchaseOrders_CreateInvalidQuantityOrPrice(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	supplier := createSupplier(t, adminToken, map[string]any{"name": "Toko Emas Jaya"})

	status, resp := doRequest(t, http.MethodPost, "/api/purchase-orders/", map[string]any{
		"supplier_id": supplier.ID,
		"items":       []map[string]any{{"product_id": product.ID, "quantity": 0, "purchase_price": 800000}},
	}, adminToken)
	if status != http.StatusBadRequest {
		t.Fatalf("quantity=0: expected 400, got %d (resp=%+v)", status, resp)
	}

	status, resp = doRequest(t, http.MethodPost, "/api/purchase-orders/", map[string]any{
		"supplier_id": supplier.ID,
		"items":       []map[string]any{{"product_id": product.ID, "quantity": 1, "purchase_price": 0}},
	}, adminToken)
	if status != http.StatusBadRequest {
		t.Fatalf("purchase_price=0: expected 400, got %d (resp=%+v)", status, resp)
	}
}

func TestPurchaseOrders_CreateSupplierNotFound(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)

	status, resp := doRequest(t, http.MethodPost, "/api/purchase-orders/", map[string]any{
		"supplier_id": nonexistentUUID,
		"items":       []map[string]any{{"product_id": product.ID, "quantity": 1, "purchase_price": 800000}},
	}, adminToken)
	if status != http.StatusNotFound {
		t.Fatalf("expected 404, got %d (resp=%+v)", status, resp)
	}
}

func TestPurchaseOrders_CreateProductNotFound(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	supplier := createSupplier(t, adminToken, map[string]any{"name": "Toko Emas Jaya"})

	status, resp := doRequest(t, http.MethodPost, "/api/purchase-orders/", map[string]any{
		"supplier_id": supplier.ID,
		"items":       []map[string]any{{"product_id": nonexistentUUID, "quantity": 1, "purchase_price": 800000}},
	}, adminToken)
	if status != http.StatusNotFound {
		t.Fatalf("expected 404, got %d (resp=%+v)", status, resp)
	}
}

func TestPurchaseOrders_CreateArchivedProductRejected(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	supplier := createSupplier(t, adminToken, map[string]any{"name": "Toko Emas Jaya"})

	status, resp := doRequest(t, http.MethodDelete, "/api/products/"+product.ID, nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("archive product: expected 200, got %d (resp=%+v)", status, resp)
	}

	status, resp = doRequest(t, http.MethodPost, "/api/purchase-orders/", map[string]any{
		"supplier_id": supplier.ID,
		"items":       []map[string]any{{"product_id": product.ID, "quantity": 1, "purchase_price": 800000}},
	}, adminToken)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (resp=%+v)", status, resp)
	}
}

// --- BE-902: list & detail ---

func TestPurchaseOrders_ListRequiresAuth(t *testing.T) {
	resetDB(t)

	status, _ := doRequest(t, http.MethodGet, "/api/purchase-orders/", nil, "")
	if status != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", status)
	}
}

func TestPurchaseOrders_ListNonAdminForbidden(t *testing.T) {
	resetDB(t)
	kasir := seedUser(t, "KASIR", true)
	token := login(t, kasir.Email, kasir.Password)

	status, resp := doRequest(t, http.MethodGet, "/api/purchase-orders/", nil, token)
	if status != http.StatusForbidden {
		t.Fatalf("expected 403, got %d (resp=%+v)", status, resp)
	}
}

func TestPurchaseOrders_ListFiltersByStatus(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	supplier := createSupplier(t, adminToken, map[string]any{"name": "Toko Emas Jaya"})

	items := []map[string]any{{"product_id": product.ID, "quantity": 1, "purchase_price": 800000}}
	pending := createPurchaseOrder(t, adminToken, supplier.ID, items)
	cancelled := createPurchaseOrder(t, adminToken, supplier.ID, items)

	status, resp := doRequest(t, http.MethodPost, "/api/purchase-orders/"+cancelled.ID+"/cancel", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("cancel: expected 200, got %d (resp=%+v)", status, resp)
	}

	status, resp = doRequest(t, http.MethodGet, "/api/purchase-orders/?status=BELUM_DITERIMA", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (resp=%+v)", status, resp)
	}
	var list purchaseOrderListDTO
	decodeData(t, resp, &list)
	if list.Pagination.Total != 1 || list.Items[0].ID != pending.ID {
		t.Fatalf("expected only pending PO, got %+v", list.Items)
	}

	status, resp = doRequest(t, http.MethodGet, "/api/purchase-orders/?status=DIBATALKAN", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (resp=%+v)", status, resp)
	}
	var cancelledList purchaseOrderListDTO
	decodeData(t, resp, &cancelledList)
	if cancelledList.Pagination.Total != 1 || cancelledList.Items[0].ID != cancelled.ID {
		t.Fatalf("expected only cancelled PO, got %+v", cancelledList.Items)
	}
}

func TestPurchaseOrders_ListPagination(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	supplier := createSupplier(t, adminToken, map[string]any{"name": "Toko Emas Jaya"})
	items := []map[string]any{{"product_id": product.ID, "quantity": 1, "purchase_price": 800000}}

	for i := 0; i < 3; i++ {
		createPurchaseOrder(t, adminToken, supplier.ID, items)
	}

	status, resp := doRequest(t, http.MethodGet, "/api/purchase-orders/?limit=2&page=1", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("page 1: expected 200, got %d (resp=%+v)", status, resp)
	}
	var page1 purchaseOrderListDTO
	decodeData(t, resp, &page1)
	if len(page1.Items) != 2 || page1.Pagination.Total != 3 || page1.Pagination.TotalPages != 2 {
		t.Fatalf("page 1: expected 2 items total=3 total_pages=2, got %d items %+v", len(page1.Items), page1.Pagination)
	}
}

func TestPurchaseOrders_GetDetailReturnsItems(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	supplier := createSupplier(t, adminToken, map[string]any{"name": "Toko Emas Jaya"})
	created := createPurchaseOrder(t, adminToken, supplier.ID, []map[string]any{
		{"product_id": product.ID, "quantity": 2, "purchase_price": 800000},
	})

	status, resp := doRequest(t, http.MethodGet, "/api/purchase-orders/"+created.ID, nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (resp=%+v)", status, resp)
	}
	var fetched purchaseOrderDTO
	decodeData(t, resp, &fetched)
	if fetched.POCode != created.POCode || len(fetched.Items) != 1 {
		t.Fatalf("expected matching detail with 1 item, got %+v", fetched)
	}
	if fetched.Items[0].Product.ID != product.ID || fetched.Items[0].Product.Name != product.Name {
		t.Fatalf("expected item product ref populated, got %+v", fetched.Items[0])
	}
	if fetched.Supplier.ID != supplier.ID || fetched.Supplier.Name != "Toko Emas Jaya" {
		t.Fatalf("expected supplier ref populated, got %+v", fetched.Supplier)
	}
}

func TestPurchaseOrders_GetNotFound(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	status, resp := doRequest(t, http.MethodGet, "/api/purchase-orders/"+nonexistentUUID, nil, adminToken)
	if status != http.StatusNotFound {
		t.Fatalf("expected 404, got %d (resp=%+v)", status, resp)
	}
}

func TestPurchaseOrders_GetInvalidIDFormat(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	status, resp := doRequest(t, http.MethodGet, "/api/purchase-orders/1", nil, adminToken)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (resp=%+v)", status, resp)
	}
}

// --- BE-903: receive ---

func TestPurchaseOrders_ReceiveRequiresAuth(t *testing.T) {
	resetDB(t)

	status, _ := doRequest(t, http.MethodPost, "/api/purchase-orders/"+nonexistentUUID+"/receive", nil, "")
	if status != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", status)
	}
}

func TestPurchaseOrders_ReceiveNonAdminForbidden(t *testing.T) {
	resetDB(t)
	kasir := seedUser(t, "KASIR", true)
	token := login(t, kasir.Email, kasir.Password)

	status, resp := doRequest(t, http.MethodPost, "/api/purchase-orders/"+nonexistentUUID+"/receive", nil, token)
	if status != http.StatusForbidden {
		t.Fatalf("expected 403, got %d (resp=%+v)", status, resp)
	}
}

func TestPurchaseOrders_ReceiveSuccess(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	supplier := createSupplier(t, adminToken, map[string]any{"name": "Toko Emas Jaya"})
	po := createPurchaseOrder(t, adminToken, supplier.ID, []map[string]any{
		{"product_id": product.ID, "quantity": 2, "purchase_price": 800000},
	})

	// Mixed conditions in the same shipment — not every unit is guaranteed
	// to arrive GOOD, so condition is per-serial, not per-item.
	status, resp := doRequest(t, http.MethodPost, "/api/purchase-orders/"+po.ID+"/receive", map[string]any{
		"items": []map[string]any{
			{"product_id": product.ID, "serials": []map[string]any{
				{"serial_number": "PO-SN-1", "condition": "GOOD"},
				{"serial_number": "PO-SN-2", "condition": "BAD"},
			}},
		},
	}, adminToken)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (resp=%+v)", status, resp)
	}
	var received purchaseOrderDTO
	decodeData(t, resp, &received)

	if received.Status != "DITERIMA" {
		t.Fatalf("expected status DITERIMA, got %q", received.Status)
	}
	if received.ReceivedAt == nil {
		t.Fatal("expected received_at to be set")
	}
	if len(received.ReceivedUnits) != 2 {
		t.Fatalf("expected 2 received_units, got %d", len(received.ReceivedUnits))
	}

	expectedConditions := map[string]string{"PO-SN-1": "GOOD", "PO-SN-2": "BAD"}
	seenBarcodes := map[string]bool{}
	for _, unit := range received.ReceivedUnits {
		if seenBarcodes[unit.Barcode] {
			t.Fatalf("expected unique barcodes, got duplicate %q", unit.Barcode)
		}
		seenBarcodes[unit.Barcode] = true

		if unit.Condition != expectedConditions[unit.SerialNumber] {
			t.Fatalf("expected serial %q to have condition %q, got %q", unit.SerialNumber, expectedConditions[unit.SerialNumber], unit.Condition)
		}

		status, resp := doRequest(t, http.MethodGet, "/api/stock-items/"+unit.StockItemID, nil, adminToken)
		if status != http.StatusOK {
			t.Fatalf("get stock item: expected 200, got %d (resp=%+v)", status, resp)
		}
		var stockItem stockItemDTO
		decodeData(t, resp, &stockItem)
		if stockItem.Status != "AVAILABLE" {
			t.Fatalf("expected AVAILABLE, got %q", stockItem.Status)
		}
		if stockItem.Condition != expectedConditions[unit.SerialNumber] {
			t.Fatalf("expected stock item condition %q, got %q", expectedConditions[unit.SerialNumber], stockItem.Condition)
		}
		if stockItem.PurchasePrice != 800000 {
			t.Fatalf("expected purchase_price=800000, got %v", stockItem.PurchasePrice)
		}

		var poID, supplierID *int64
		if err := testPool.QueryRow(context.Background(),
			`SELECT po_id, supplier_id FROM stock_items WHERE public_id = $1`, unit.StockItemID,
		).Scan(&poID, &supplierID); err != nil {
			t.Fatalf("query stock item po_id/supplier_id: %v", err)
		}
		if poID == nil || supplierID == nil {
			t.Fatalf("expected po_id/supplier_id set on received unit, got po_id=%v supplier_id=%v", poID, supplierID)
		}
	}
}

func TestPurchaseOrders_ReceiveWrongSerialCount(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	supplier := createSupplier(t, adminToken, map[string]any{"name": "Toko Emas Jaya"})
	po := createPurchaseOrder(t, adminToken, supplier.ID, []map[string]any{
		{"product_id": product.ID, "quantity": 2, "purchase_price": 800000},
	})

	status, resp := doRequest(t, http.MethodPost, "/api/purchase-orders/"+po.ID+"/receive", map[string]any{
		"items": []map[string]any{
			{"product_id": product.ID, "serials": []map[string]any{{"serial_number": "PO-SN-1", "condition": "GOOD"}}},
		},
	}, adminToken)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400 for serial count mismatch, got %d (resp=%+v)", status, resp)
	}
}

func TestPurchaseOrders_ReceiveMissingProductCoverage(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product1 := stockItemFixtureProduct(t, adminToken)
	category2 := createCategory(t, adminToken, "Koin")
	brand2 := createBrand(t, adminToken, "UBS")
	product2 := createProduct(t, adminToken, "Koin Emas 5gr", category2.ID, brand2.ID, 5)
	supplier := createSupplier(t, adminToken, map[string]any{"name": "Toko Emas Jaya"})

	po := createPurchaseOrder(t, adminToken, supplier.ID, []map[string]any{
		{"product_id": product1.ID, "quantity": 1, "purchase_price": 800000},
		{"product_id": product2.ID, "quantity": 1, "purchase_price": 500000},
	})

	status, resp := doRequest(t, http.MethodPost, "/api/purchase-orders/"+po.ID+"/receive", map[string]any{
		"items": []map[string]any{
			{"product_id": product1.ID, "serials": []map[string]any{{"serial_number": "PO-SN-COVER-1", "condition": "GOOD"}}},
		},
	}, adminToken)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing product coverage, got %d (resp=%+v)", status, resp)
	}
}

func TestPurchaseOrders_ReceiveEmptySerial(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	supplier := createSupplier(t, adminToken, map[string]any{"name": "Toko Emas Jaya"})
	po := createPurchaseOrder(t, adminToken, supplier.ID, []map[string]any{
		{"product_id": product.ID, "quantity": 1, "purchase_price": 800000},
	})

	status, resp := doRequest(t, http.MethodPost, "/api/purchase-orders/"+po.ID+"/receive", map[string]any{
		"items": []map[string]any{
			{"product_id": product.ID, "serials": []map[string]any{{"serial_number": "", "condition": "GOOD"}}},
		},
	}, adminToken)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d (resp=%+v)", status, resp)
	}
}

func TestPurchaseOrders_ReceiveInvalidCondition(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	supplier := createSupplier(t, adminToken, map[string]any{"name": "Toko Emas Jaya"})
	po := createPurchaseOrder(t, adminToken, supplier.ID, []map[string]any{
		{"product_id": product.ID, "quantity": 1, "purchase_price": 800000},
	})

	status, resp := doRequest(t, http.MethodPost, "/api/purchase-orders/"+po.ID+"/receive", map[string]any{
		"items": []map[string]any{
			{"product_id": product.ID, "serials": []map[string]any{{"serial_number": "PO-SN-COND", "condition": "OK"}}},
		},
	}, adminToken)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d (resp=%+v)", status, resp)
	}
}

func TestPurchaseOrders_ReceiveDuplicateSerialInBatch(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	supplier := createSupplier(t, adminToken, map[string]any{"name": "Toko Emas Jaya"})
	po := createPurchaseOrder(t, adminToken, supplier.ID, []map[string]any{
		{"product_id": product.ID, "quantity": 2, "purchase_price": 800000},
	})

	status, resp := doRequest(t, http.MethodPost, "/api/purchase-orders/"+po.ID+"/receive", map[string]any{
		"items": []map[string]any{
			{"product_id": product.ID, "serials": []map[string]any{{"serial_number": "DUP-SN", "condition": "GOOD"}, {"serial_number": "DUP-SN", "condition": "GOOD"}}},
		},
	}, adminToken)
	if status != http.StatusConflict {
		t.Fatalf("expected 409, got %d (resp=%+v)", status, resp)
	}
}

func TestPurchaseOrders_ReceiveAlreadyReceivedRejected(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	supplier := createSupplier(t, adminToken, map[string]any{"name": "Toko Emas Jaya"})
	po := createPurchaseOrder(t, adminToken, supplier.ID, []map[string]any{
		{"product_id": product.ID, "quantity": 1, "purchase_price": 800000},
	})

	body := map[string]any{
		"items": []map[string]any{
			{"product_id": product.ID, "serials": []map[string]any{{"serial_number": "ONCE-SN", "condition": "GOOD"}}},
		},
	}
	status, resp := doRequest(t, http.MethodPost, "/api/purchase-orders/"+po.ID+"/receive", body, adminToken)
	if status != http.StatusOK {
		t.Fatalf("first receive: expected 200, got %d (resp=%+v)", status, resp)
	}

	status, resp = doRequest(t, http.MethodPost, "/api/purchase-orders/"+po.ID+"/receive", map[string]any{
		"items": []map[string]any{
			{"product_id": product.ID, "serials": []map[string]any{{"serial_number": "TWICE-SN", "condition": "GOOD"}}},
		},
	}, adminToken)
	if status != http.StatusConflict {
		t.Fatalf("second receive: expected 409, got %d (resp=%+v)", status, resp)
	}
}

func TestPurchaseOrders_ReceiveNotFound(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	status, resp := doRequest(t, http.MethodPost, "/api/purchase-orders/"+nonexistentUUID+"/receive", map[string]any{
		"items": []map[string]any{},
	}, adminToken)
	if status != http.StatusNotFound {
		t.Fatalf("expected 404, got %d (resp=%+v)", status, resp)
	}
}

// --- BE-904: cancel ---

func TestPurchaseOrders_CancelRequiresAuth(t *testing.T) {
	resetDB(t)

	status, _ := doRequest(t, http.MethodPost, "/api/purchase-orders/"+nonexistentUUID+"/cancel", nil, "")
	if status != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", status)
	}
}

func TestPurchaseOrders_CancelNonAdminForbidden(t *testing.T) {
	resetDB(t)
	kasir := seedUser(t, "KASIR", true)
	token := login(t, kasir.Email, kasir.Password)

	status, resp := doRequest(t, http.MethodPost, "/api/purchase-orders/"+nonexistentUUID+"/cancel", nil, token)
	if status != http.StatusForbidden {
		t.Fatalf("expected 403, got %d (resp=%+v)", status, resp)
	}
}

func TestPurchaseOrders_CancelSuccess(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	supplier := createSupplier(t, adminToken, map[string]any{"name": "Toko Emas Jaya"})
	po := createPurchaseOrder(t, adminToken, supplier.ID, []map[string]any{
		{"product_id": product.ID, "quantity": 1, "purchase_price": 800000},
	})

	status, resp := doRequest(t, http.MethodPost, "/api/purchase-orders/"+po.ID+"/cancel", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (resp=%+v)", status, resp)
	}

	status, resp = doRequest(t, http.MethodGet, "/api/purchase-orders/"+po.ID, nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("get: expected 200, got %d (resp=%+v)", status, resp)
	}
	var fetched purchaseOrderDTO
	decodeData(t, resp, &fetched)
	if fetched.Status != "DIBATALKAN" {
		t.Fatalf("expected status DIBATALKAN, got %q", fetched.Status)
	}
}

func TestPurchaseOrders_CancelAlreadyReceivedRejected(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	supplier := createSupplier(t, adminToken, map[string]any{"name": "Toko Emas Jaya"})
	po := createPurchaseOrder(t, adminToken, supplier.ID, []map[string]any{
		{"product_id": product.ID, "quantity": 1, "purchase_price": 800000},
	})

	status, resp := doRequest(t, http.MethodPost, "/api/purchase-orders/"+po.ID+"/receive", map[string]any{
		"items": []map[string]any{
			{"product_id": product.ID, "serials": []map[string]any{{"serial_number": "RECV-SN", "condition": "GOOD"}}},
		},
	}, adminToken)
	if status != http.StatusOK {
		t.Fatalf("receive: expected 200, got %d (resp=%+v)", status, resp)
	}

	status, resp = doRequest(t, http.MethodPost, "/api/purchase-orders/"+po.ID+"/cancel", nil, adminToken)
	if status != http.StatusConflict {
		t.Fatalf("expected 409, got %d (resp=%+v)", status, resp)
	}
}

func TestPurchaseOrders_CancelAlreadyCancelledRejected(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	supplier := createSupplier(t, adminToken, map[string]any{"name": "Toko Emas Jaya"})
	po := createPurchaseOrder(t, adminToken, supplier.ID, []map[string]any{
		{"product_id": product.ID, "quantity": 1, "purchase_price": 800000},
	})

	status, resp := doRequest(t, http.MethodPost, "/api/purchase-orders/"+po.ID+"/cancel", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("first cancel: expected 200, got %d (resp=%+v)", status, resp)
	}

	status, resp = doRequest(t, http.MethodPost, "/api/purchase-orders/"+po.ID+"/cancel", nil, adminToken)
	if status != http.StatusConflict {
		t.Fatalf("second cancel: expected 409, got %d (resp=%+v)", status, resp)
	}
}

func TestPurchaseOrders_CancelNotFound(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	status, resp := doRequest(t, http.MethodPost, "/api/purchase-orders/"+nonexistentUUID+"/cancel", nil, adminToken)
	if status != http.StatusNotFound {
		t.Fatalf("expected 404, got %d (resp=%+v)", status, resp)
	}
}
