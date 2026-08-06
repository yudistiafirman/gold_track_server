package e2e

import (
	"net/http"
	"testing"
)

type balanceAccountDTO struct {
	ID      string  `json:"id"`
	Name    string  `json:"name"`
	Balance float64 `json:"balance"`
}

func createBalanceAccount(t *testing.T, token, name string, balance float64) balanceAccountDTO {
	t.Helper()
	status, resp := doRequest(t, http.MethodPost, "/api/balance-accounts/", map[string]any{"name": name, "balance": balance}, token)
	if status != http.StatusCreated {
		t.Fatalf("create balance account fixture: expected 201, got %d (resp=%+v)", status, resp)
	}
	var a balanceAccountDTO
	decodeData(t, resp, &a)
	return a
}

func TestBalanceAccounts_RequireAuth(t *testing.T) {
	resetDB(t)

	cases := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/balance-accounts/"},
		{http.MethodPost, "/api/balance-accounts/"},
		{http.MethodGet, "/api/balance-accounts/" + nonexistentUUID},
		{http.MethodPut, "/api/balance-accounts/" + nonexistentUUID},
		{http.MethodDelete, "/api/balance-accounts/" + nonexistentUUID},
	}
	for _, c := range cases {
		status, _ := doRequest(t, c.method, c.path, nil, "")
		if status != http.StatusUnauthorized {
			t.Errorf("%s %s without token: expected 401, got %d", c.method, c.path, status)
		}
	}
}

// This resource is SUPER_ADMIN-only — stricter than most resources in this
// app (ADMIN+SUPER_ADMIN), since plain ADMIN shouldn't see where the shop's
// money lives (client requirement).
func TestBalanceAccounts_NonSuperAdminForbidden(t *testing.T) {
	resetDB(t)
	kasir := seedUser(t, "KASIR", true)
	kasirToken := login(t, kasir.Email, kasir.Password)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	for _, tc := range []struct {
		role  string
		token string
	}{{"KASIR", kasirToken}, {"ADMIN", adminToken}} {
		status, resp := doRequest(t, http.MethodGet, "/api/balance-accounts/", nil, tc.token)
		if status != http.StatusForbidden {
			t.Errorf("list as %s: expected 403, got %d (resp=%+v)", tc.role, status, resp)
		}
		status, resp = doRequest(t, http.MethodPost, "/api/balance-accounts/", map[string]any{"name": "X", "balance": 1}, tc.token)
		if status != http.StatusForbidden {
			t.Errorf("create as %s: expected 403, got %d (resp=%+v)", tc.role, status, resp)
		}
	}
}

func TestBalanceAccounts_CreateListGetUpdateDelete(t *testing.T) {
	resetDB(t)
	superAdmin := seedUser(t, "SUPER_ADMIN", true)
	token := login(t, superAdmin.Email, superAdmin.Password)

	status, resp := doRequest(t, http.MethodPost, "/api/balance-accounts/", map[string]any{"name": "BCA Bisnis", "balance": 1000000}, token)
	if status != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d (resp=%+v)", status, resp)
	}
	var created balanceAccountDTO
	decodeData(t, resp, &created)
	if created.ID == "" || created.Name != "BCA Bisnis" || created.Balance != 1000000 {
		t.Fatalf("create: unexpected response %+v", created)
	}

	status, resp = doRequest(t, http.MethodGet, "/api/balance-accounts/", nil, token)
	if status != http.StatusOK {
		t.Fatalf("list: expected 200, got %d", status)
	}
	var list []balanceAccountDTO
	decodeData(t, resp, &list)
	found := false
	for _, a := range list {
		if a.ID == created.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("list: expected created account %s in list", created.ID)
	}

	status, resp = doRequest(t, http.MethodGet, "/api/balance-accounts/"+created.ID, nil, token)
	if status != http.StatusOK {
		t.Fatalf("get: expected 200, got %d", status)
	}

	// PUT overwriting the balance directly is the mechanism for manual
	// updates (e.g. a deposit) — there is no separate history/transaction
	// log for this resource (client requirement).
	status, resp = doRequest(t, http.MethodPut, "/api/balance-accounts/"+created.ID, map[string]any{"name": "BCA Bisnis", "balance": 1500000}, token)
	if status != http.StatusOK {
		t.Fatalf("update: expected 200, got %d (resp=%+v)", status, resp)
	}
	var updated balanceAccountDTO
	decodeData(t, resp, &updated)
	if updated.Balance != 1500000 {
		t.Fatalf("update: balance not applied, got %+v", updated)
	}

	// Delete is a real hard delete — no is_active column, no history kept.
	status, resp = doRequest(t, http.MethodDelete, "/api/balance-accounts/"+created.ID, nil, token)
	if status != http.StatusOK {
		t.Fatalf("delete: expected 200, got %d (resp=%+v)", status, resp)
	}

	status, resp = doRequest(t, http.MethodGet, "/api/balance-accounts/"+created.ID, nil, token)
	if status != http.StatusNotFound {
		t.Fatalf("get after delete: expected 404 (hard delete), got %d (resp=%+v)", status, resp)
	}
}

func TestBalanceAccounts_CreateDuplicateNameConflict(t *testing.T) {
	resetDB(t)
	superAdmin := seedUser(t, "SUPER_ADMIN", true)
	token := login(t, superAdmin.Email, superAdmin.Password)

	createBalanceAccount(t, token, "Cash", 0)

	status, resp := doRequest(t, http.MethodPost, "/api/balance-accounts/", map[string]any{"name": "Cash", "balance": 0}, token)
	if status != http.StatusConflict {
		t.Fatalf("expected 409, got %d (resp=%+v)", status, resp)
	}
}

func TestBalanceAccounts_UpdateDuplicateNameConflict(t *testing.T) {
	resetDB(t)
	superAdmin := seedUser(t, "SUPER_ADMIN", true)
	token := login(t, superAdmin.Email, superAdmin.Password)

	createBalanceAccount(t, token, "BCA Bisnis", 0)
	other := createBalanceAccount(t, token, "Mandiri Bisnis", 0)

	status, resp := doRequest(t, http.MethodPut, "/api/balance-accounts/"+other.ID, map[string]any{"name": "BCA Bisnis", "balance": 0}, token)
	if status != http.StatusConflict {
		t.Fatalf("expected 409, got %d (resp=%+v)", status, resp)
	}
}

func TestBalanceAccounts_CreateEmptyNameBadRequest(t *testing.T) {
	resetDB(t)
	superAdmin := seedUser(t, "SUPER_ADMIN", true)
	token := login(t, superAdmin.Email, superAdmin.Password)

	status, resp := doRequest(t, http.MethodPost, "/api/balance-accounts/", map[string]any{"name": "", "balance": 0}, token)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (resp=%+v)", status, resp)
	}
}

func TestBalanceAccounts_CreateNegativeBalanceBadRequest(t *testing.T) {
	resetDB(t)
	superAdmin := seedUser(t, "SUPER_ADMIN", true)
	token := login(t, superAdmin.Email, superAdmin.Password)

	status, resp := doRequest(t, http.MethodPost, "/api/balance-accounts/", map[string]any{"name": "Cash", "balance": -100}, token)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (resp=%+v)", status, resp)
	}
}

func TestBalanceAccounts_GetNotFound(t *testing.T) {
	resetDB(t)
	superAdmin := seedUser(t, "SUPER_ADMIN", true)
	token := login(t, superAdmin.Email, superAdmin.Password)

	status, resp := doRequest(t, http.MethodGet, "/api/balance-accounts/"+nonexistentUUID, nil, token)
	if status != http.StatusNotFound {
		t.Fatalf("expected 404, got %d (resp=%+v)", status, resp)
	}
}

func TestBalanceAccounts_GetInvalidIDFormat(t *testing.T) {
	resetDB(t)
	superAdmin := seedUser(t, "SUPER_ADMIN", true)
	token := login(t, superAdmin.Email, superAdmin.Password)

	status, resp := doRequest(t, http.MethodGet, "/api/balance-accounts/1", nil, token)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (resp=%+v)", status, resp)
	}
}

func TestBalanceAccounts_DeleteNotFound(t *testing.T) {
	resetDB(t)
	superAdmin := seedUser(t, "SUPER_ADMIN", true)
	token := login(t, superAdmin.Email, superAdmin.Password)

	status, resp := doRequest(t, http.MethodDelete, "/api/balance-accounts/"+nonexistentUUID, nil, token)
	if status != http.StatusNotFound {
		t.Fatalf("expected 404, got %d (resp=%+v)", status, resp)
	}
}
