package e2e

import (
	"context"
	"net/http"
	"testing"
	"time"
)

type dailyClosingDTO struct {
	ID             string  `json:"id"`
	ClosingDate    string  `json:"closing_date"`
	TotalBalance   float64 `json:"total_balance"`
	TotalGoldValue float64 `json:"total_gold_value"`
	TotalSaldo     float64 `json:"total_saldo"`
}

type dailyClosingListDTO struct {
	Items      []dailyClosingDTO `json:"items"`
	Pagination paginationDTO     `json:"pagination"`
}

type reconciliationDTO struct {
	HasBaseline          bool    `json:"has_baseline"`
	LastClosingDate      string  `json:"last_closing_date"`
	PeriodFrom           string  `json:"period_from"`
	PeriodTo             string  `json:"period_to"`
	LastClosingSaldo     float64 `json:"last_closing_saldo"`
	PeriodRevenue        float64 `json:"period_revenue"`
	PeriodCOGS           float64 `json:"period_cogs"`
	PeriodExpenses       float64 `json:"period_expenses"`
	PeriodNetProfit      float64 `json:"period_net_profit"`
	ActualTotalBalance   float64 `json:"actual_total_balance"`
	ActualTotalGoldValue float64 `json:"actual_total_gold_value"`
	ActualSaldo          float64 `json:"actual_saldo"`
	ExpectedSaldo        float64 `json:"expected_saldo"`
	Difference           float64 `json:"difference"`
	InSync               bool    `json:"in_sync"`
}

func closeDailyBalance(t *testing.T, token string) dailyClosingDTO {
	t.Helper()
	status, resp := doRequest(t, http.MethodPost, "/api/daily-closings/", nil, token)
	if status != http.StatusCreated {
		t.Fatalf("close daily balance fixture: expected 201, got %d (resp=%+v)", status, resp)
	}
	var c dailyClosingDTO
	decodeData(t, resp, &c)
	return c
}

// setDailyClosingDate backdates a closing's closing_date directly via SQL —
// same "reach into the DB when no endpoint exposes something" pattern as
// setTransactionCreatedAt (reports_test.go), needed here because the API
// always closes "today" and e2e tests can't fast-forward wall-clock time.
func setDailyClosingDate(t *testing.T, publicID string, date time.Time) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(), `UPDATE daily_closings SET closing_date = $1 WHERE public_id = $2`, date, publicID); err != nil {
		t.Fatalf("backdate daily closing: %v", err)
	}
}

func TestDailyClosings_RequireAuth(t *testing.T) {
	resetDB(t)

	cases := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/daily-closings/"},
		{http.MethodPost, "/api/daily-closings/"},
		{http.MethodGet, "/api/daily-closings/" + nonexistentUUID},
	}
	for _, c := range cases {
		status, _ := doRequest(t, c.method, c.path, nil, "")
		if status != http.StatusUnauthorized {
			t.Errorf("%s %s without token: expected 401, got %d", c.method, c.path, status)
		}
	}
}

// Same tier as reports/balance-accounts — plain ADMIN doesn't get visibility
// into where the shop's money lives (client requirement).
func TestDailyClosings_NonSuperAdminForbidden(t *testing.T) {
	resetDB(t)
	kasir := seedUser(t, "KASIR", true)
	kasirToken := login(t, kasir.Email, kasir.Password)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	for _, tc := range []struct {
		role  string
		token string
	}{{"KASIR", kasirToken}, {"ADMIN", adminToken}} {
		status, resp := doRequest(t, http.MethodGet, "/api/daily-closings/", nil, tc.token)
		if status != http.StatusForbidden {
			t.Errorf("list as %s: expected 403, got %d (resp=%+v)", tc.role, status, resp)
		}
		status, resp = doRequest(t, http.MethodPost, "/api/daily-closings/", nil, tc.token)
		if status != http.StatusForbidden {
			t.Errorf("create as %s: expected 403, got %d (resp=%+v)", tc.role, status, resp)
		}
	}
}

func TestDailyClosings_CreateListGet(t *testing.T) {
	resetDB(t)
	superAdmin := seedUser(t, "SUPER_ADMIN", true)
	token := login(t, superAdmin.Email, superAdmin.Password)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	product := stockItemFixtureProduct(t, adminToken)
	createStockItemAPI(t, adminToken, product.ID, validStockItemBody(map[string]any{"serial_number": "DC-1", "purchase_price": 2000000}))
	createBalanceAccount(t, token, "Cash", 750000)

	created := closeDailyBalance(t, token)
	if created.ID == "" {
		t.Fatalf("create: expected non-empty id, got %+v", created)
	}
	wantToday := time.Now().UTC().Format("2006-01-02")
	if created.ClosingDate != wantToday {
		t.Fatalf("create: expected closing_date=%s (today, UTC), got %s", wantToday, created.ClosingDate)
	}
	if created.TotalGoldValue != 2000000 || created.TotalBalance != 750000 || created.TotalSaldo != 2750000 {
		t.Fatalf("create: expected gold=2000000 balance=750000 saldo=2750000 (matching live cash summary), got %+v", created)
	}

	status, resp := doRequest(t, http.MethodGet, "/api/daily-closings/", nil, token)
	if status != http.StatusOK {
		t.Fatalf("list: expected 200, got %d (resp=%+v)", status, resp)
	}
	var list dailyClosingListDTO
	decodeData(t, resp, &list)
	if len(list.Items) != 1 || list.Items[0].ID != created.ID {
		t.Fatalf("list: expected the created closing, got %+v", list.Items)
	}

	status, resp = doRequest(t, http.MethodGet, "/api/daily-closings/"+created.ID, nil, token)
	if status != http.StatusOK {
		t.Fatalf("get: expected 200, got %d (resp=%+v)", status, resp)
	}
	var fetched dailyClosingDTO
	decodeData(t, resp, &fetched)
	if fetched.TotalSaldo != created.TotalSaldo {
		t.Fatalf("get: expected same saldo as create response, got %+v", fetched)
	}
}

func TestDailyClosings_CreateDuplicateSameDayConflict(t *testing.T) {
	resetDB(t)
	superAdmin := seedUser(t, "SUPER_ADMIN", true)
	token := login(t, superAdmin.Email, superAdmin.Password)

	closeDailyBalance(t, token)

	status, resp := doRequest(t, http.MethodPost, "/api/daily-closings/", nil, token)
	if status != http.StatusConflict {
		t.Fatalf("expected 409 closing an already-closed day, got %d (resp=%+v)", status, resp)
	}
}

func TestDailyClosings_GetNotFound(t *testing.T) {
	resetDB(t)
	superAdmin := seedUser(t, "SUPER_ADMIN", true)
	token := login(t, superAdmin.Email, superAdmin.Password)

	status, resp := doRequest(t, http.MethodGet, "/api/daily-closings/"+nonexistentUUID, nil, token)
	if status != http.StatusNotFound {
		t.Fatalf("expected 404, got %d (resp=%+v)", status, resp)
	}
}

func TestDailyClosings_GetInvalidIDFormat(t *testing.T) {
	resetDB(t)
	superAdmin := seedUser(t, "SUPER_ADMIN", true)
	token := login(t, superAdmin.Email, superAdmin.Password)

	status, resp := doRequest(t, http.MethodGet, "/api/daily-closings/1", nil, token)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (resp=%+v)", status, resp)
	}
}

// --- GET /api/reports/reconciliation ---

func TestReconciliation_RequireAuth(t *testing.T) {
	resetDB(t)

	status, _ := doRequest(t, http.MethodGet, "/api/reports/reconciliation", nil, "")
	if status != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", status)
	}
}

func TestReconciliation_NonSuperAdminForbidden(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	status, resp := doRequest(t, http.MethodGet, "/api/reports/reconciliation", nil, adminToken)
	if status != http.StatusForbidden {
		t.Fatalf("expected 403, got %d (resp=%+v)", status, resp)
	}
}

func TestReconciliation_NoBaselineYet(t *testing.T) {
	resetDB(t)
	superAdmin := seedUser(t, "SUPER_ADMIN", true)
	token := login(t, superAdmin.Email, superAdmin.Password)
	createBalanceAccount(t, token, "Cash", 500000)

	status, resp := doRequest(t, http.MethodGet, "/api/reports/reconciliation", nil, token)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (resp=%+v)", status, resp)
	}
	var recon reconciliationDTO
	decodeData(t, resp, &recon)

	if recon.HasBaseline {
		t.Fatalf("expected has_baseline=false (no closing ever recorded), got %+v", recon)
	}
	if recon.ActualTotalBalance != 500000 || recon.ActualSaldo != 500000 {
		t.Fatalf("expected actual_saldo to still reflect the live cash summary, got %+v", recon)
	}
}

// TestReconciliation_InSyncWhenSaleProceedsAreRecorded closes a baseline day
// (backdated to yesterday), then sells the one AVAILABLE unit and manually
// deposits the sale proceeds into the tracked balance account — exactly
// what a diligent owner does in the client's Excel. With the cash properly
// recorded, today's live saldo should exactly match the saldo the last
// closing plus today's booked profit would predict.
func TestReconciliation_InSyncWhenSaleProceedsAreRecorded(t *testing.T) {
	resetDB(t)
	superAdmin := seedUser(t, "SUPER_ADMIN", true)
	token := login(t, superAdmin.Email, superAdmin.Password)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	product := stockItemFixtureProduct(t, adminToken)
	item := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(map[string]any{"serial_number": "RECON-1"})) // purchase_price 1000000
	cashAccount := createBalanceAccount(t, token, "Cash", 500000)

	baseline := closeDailyBalance(t, token) // saldo = gold(1000000) + balance(500000) = 1500000
	yesterday := time.Now().UTC().AddDate(0, 0, -1)
	setDailyClosingDate(t, baseline.ID, yesterday)

	customer := createCustomer(t, adminToken, map[string]any{"name": "Budi Santoso"})
	status, resp := doRequest(t, http.MethodPost, "/api/transactions", sellTransactionBody(customer.ID, []map[string]any{
		{"stock_item_id": item.ID, "price_total": 1500000},
	}), adminToken)
	if status != http.StatusCreated {
		t.Fatalf("sell: expected 201, got %d (resp=%+v)", status, resp)
	}

	// Record the sale proceeds into the tracked cash account — the step a
	// real owner does manually and that this feature exists to catch when
	// skipped.
	status, resp = doRequest(t, http.MethodPut, "/api/balance-accounts/"+cashAccount.ID, map[string]any{
		"name": "Cash", "balance": 500000 + 1500000,
	}, token)
	if status != http.StatusOK {
		t.Fatalf("deposit sale proceeds: expected 200, got %d (resp=%+v)", status, resp)
	}

	status, resp = doRequest(t, http.MethodGet, "/api/reports/reconciliation", nil, token)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (resp=%+v)", status, resp)
	}
	var recon reconciliationDTO
	decodeData(t, resp, &recon)

	wantToday := time.Now().UTC().Format("2006-01-02")
	if !recon.HasBaseline || recon.LastClosingDate != yesterday.Format("2006-01-02") || recon.PeriodFrom != wantToday || recon.PeriodTo != wantToday {
		t.Fatalf("expected baseline=yesterday period=today..today, got %+v", recon)
	}
	if recon.PeriodNetProfit != 500000 {
		t.Fatalf("expected period_net_profit=500000 (1500000 revenue - 1000000 cogs), got %v", recon.PeriodNetProfit)
	}
	if recon.Difference != 0 || !recon.InSync {
		t.Fatalf("expected difference=0 and in_sync=true (sale proceeds fully recorded), got %+v", recon)
	}
}

// TestReconciliation_DetectsUntrackedRevenue is the mirror case: the sale
// happens but nobody deposits the proceeds into a tracked balance account —
// exactly the "tidak singkron" symptom the client reported. The gap between
// expected and actual saldo should be exactly the un-recorded cash.
func TestReconciliation_DetectsUntrackedRevenue(t *testing.T) {
	resetDB(t)
	superAdmin := seedUser(t, "SUPER_ADMIN", true)
	token := login(t, superAdmin.Email, superAdmin.Password)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	product := stockItemFixtureProduct(t, adminToken)
	item := createStockItemAPI(t, adminToken, product.ID, validStockItemBody(map[string]any{"serial_number": "RECON-2"})) // purchase_price 1000000
	createBalanceAccount(t, token, "Cash", 500000)

	baseline := closeDailyBalance(t, token) // saldo = gold(1000000) + balance(500000) = 1500000
	yesterday := time.Now().UTC().AddDate(0, 0, -1)
	setDailyClosingDate(t, baseline.ID, yesterday)

	customer := createCustomer(t, adminToken, map[string]any{"name": "Budi Santoso"})
	status, resp := doRequest(t, http.MethodPost, "/api/transactions", sellTransactionBody(customer.ID, []map[string]any{
		{"stock_item_id": item.ID, "price_total": 1500000},
	}), adminToken)
	if status != http.StatusCreated {
		t.Fatalf("sell: expected 201, got %d (resp=%+v)", status, resp)
	}
	// Cash account is deliberately left untouched — the sale proceeds are
	// never recorded, mirroring the client's real discrepancy.

	status, resp = doRequest(t, http.MethodGet, "/api/reports/reconciliation", nil, token)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (resp=%+v)", status, resp)
	}
	var recon reconciliationDTO
	decodeData(t, resp, &recon)

	if recon.Difference != -1500000 || recon.InSync {
		t.Fatalf("expected difference=-1500000 (unrecorded sale proceeds) and in_sync=false, got %+v", recon)
	}
}
