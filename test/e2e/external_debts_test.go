package e2e

import (
	"net/http"
	"testing"
)

type externalDebtDTO struct {
	ID         string  `json:"id"`
	DebtorName string  `json:"debtor_name"`
	Amount     float64 `json:"amount"`
}

func createExternalDebt(t *testing.T, token, debtorName string, amount float64) externalDebtDTO {
	t.Helper()
	status, resp := doRequest(t, http.MethodPost, "/api/external-debts/", map[string]any{"debtor_name": debtorName, "amount": amount}, token)
	if status != http.StatusCreated {
		t.Fatalf("create external debt fixture: expected 201, got %d (resp=%+v)", status, resp)
	}
	var d externalDebtDTO
	decodeData(t, resp, &d)
	return d
}

func TestExternalDebts_RequireAuth(t *testing.T) {
	resetDB(t)

	cases := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/external-debts/"},
		{http.MethodPost, "/api/external-debts/"},
		{http.MethodGet, "/api/external-debts/" + nonexistentUUID},
		{http.MethodPut, "/api/external-debts/" + nonexistentUUID},
		{http.MethodDelete, "/api/external-debts/" + nonexistentUUID},
	}
	for _, c := range cases {
		status, _ := doRequest(t, c.method, c.path, nil, "")
		if status != http.StatusUnauthorized {
			t.Errorf("%s %s without token: expected 401, got %d", c.method, c.path, status)
		}
	}
}

func TestExternalDebts_NonSuperAdminForbidden(t *testing.T) {
	resetDB(t)
	kasir := seedUser(t, "KASIR", true)
	kasirToken := login(t, kasir.Email, kasir.Password)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	for _, tc := range []struct {
		role  string
		token string
	}{{"KASIR", kasirToken}, {"ADMIN", adminToken}} {
		status, resp := doRequest(t, http.MethodGet, "/api/external-debts/", nil, tc.token)
		if status != http.StatusForbidden {
			t.Errorf("list as %s: expected 403, got %d (resp=%+v)", tc.role, status, resp)
		}
		status, resp = doRequest(t, http.MethodPost, "/api/external-debts/", map[string]any{"debtor_name": "X", "amount": 1}, tc.token)
		if status != http.StatusForbidden {
			t.Errorf("create as %s: expected 403, got %d (resp=%+v)", tc.role, status, resp)
		}
	}
}

func TestExternalDebts_CreateListGetUpdateDelete(t *testing.T) {
	resetDB(t)
	superAdmin := seedUser(t, "SUPER_ADMIN", true)
	token := login(t, superAdmin.Email, superAdmin.Password)

	status, resp := doRequest(t, http.MethodPost, "/api/external-debts/", map[string]any{"debtor_name": "Budi", "amount": 2000000}, token)
	if status != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d (resp=%+v)", status, resp)
	}
	var created externalDebtDTO
	decodeData(t, resp, &created)
	if created.ID == "" || created.DebtorName != "Budi" || created.Amount != 2000000 {
		t.Fatalf("create: unexpected response %+v", created)
	}

	status, resp = doRequest(t, http.MethodGet, "/api/external-debts/", nil, token)
	if status != http.StatusOK {
		t.Fatalf("list: expected 200, got %d", status)
	}
	var list []externalDebtDTO
	decodeData(t, resp, &list)
	found := false
	for _, d := range list {
		if d.ID == created.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("list: expected created debt %s in list", created.ID)
	}

	status, resp = doRequest(t, http.MethodGet, "/api/external-debts/"+created.ID, nil, token)
	if status != http.StatusOK {
		t.Fatalf("get: expected 200, got %d", status)
	}

	// Partial repayment ("cicilan") is modeled by directly editing the
	// amount down via PUT — there's no separate payment log (client
	// requirement).
	status, resp = doRequest(t, http.MethodPut, "/api/external-debts/"+created.ID, map[string]any{"debtor_name": "Budi", "amount": 1000000}, token)
	if status != http.StatusOK {
		t.Fatalf("update: expected 200, got %d (resp=%+v)", status, resp)
	}
	var updated externalDebtDTO
	decodeData(t, resp, &updated)
	if updated.Amount != 1000000 {
		t.Fatalf("update: amount not applied (cicilan paydown), got %+v", updated)
	}

	// Fully paid off → deleted outright, no status field, no history.
	status, resp = doRequest(t, http.MethodDelete, "/api/external-debts/"+created.ID, nil, token)
	if status != http.StatusOK {
		t.Fatalf("delete: expected 200, got %d (resp=%+v)", status, resp)
	}

	status, resp = doRequest(t, http.MethodGet, "/api/external-debts/"+created.ID, nil, token)
	if status != http.StatusNotFound {
		t.Fatalf("get after delete: expected 404 (hard delete), got %d (resp=%+v)", status, resp)
	}
}

func TestExternalDebts_CreateEmptyDebtorNameBadRequest(t *testing.T) {
	resetDB(t)
	superAdmin := seedUser(t, "SUPER_ADMIN", true)
	token := login(t, superAdmin.Email, superAdmin.Password)

	status, resp := doRequest(t, http.MethodPost, "/api/external-debts/", map[string]any{"debtor_name": "", "amount": 1000000}, token)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (resp=%+v)", status, resp)
	}
}

func TestExternalDebts_CreateZeroOrNegativeAmountBadRequest(t *testing.T) {
	resetDB(t)
	superAdmin := seedUser(t, "SUPER_ADMIN", true)
	token := login(t, superAdmin.Email, superAdmin.Password)

	for _, amount := range []float64{0, -100} {
		status, resp := doRequest(t, http.MethodPost, "/api/external-debts/", map[string]any{"debtor_name": "Budi", "amount": amount}, token)
		if status != http.StatusBadRequest {
			t.Errorf("amount=%v: expected 400, got %d (resp=%+v)", amount, status, resp)
		}
	}
}

func TestExternalDebts_GetNotFound(t *testing.T) {
	resetDB(t)
	superAdmin := seedUser(t, "SUPER_ADMIN", true)
	token := login(t, superAdmin.Email, superAdmin.Password)

	status, resp := doRequest(t, http.MethodGet, "/api/external-debts/"+nonexistentUUID, nil, token)
	if status != http.StatusNotFound {
		t.Fatalf("expected 404, got %d (resp=%+v)", status, resp)
	}
}

func TestExternalDebts_DeleteNotFound(t *testing.T) {
	resetDB(t)
	superAdmin := seedUser(t, "SUPER_ADMIN", true)
	token := login(t, superAdmin.Email, superAdmin.Password)

	status, resp := doRequest(t, http.MethodDelete, "/api/external-debts/"+nonexistentUUID, nil, token)
	if status != http.StatusNotFound {
		t.Fatalf("expected 404, got %d (resp=%+v)", status, resp)
	}
}
