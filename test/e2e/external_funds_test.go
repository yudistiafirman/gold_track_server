package e2e

import (
	"net/http"
	"testing"
)

type externalFundDTO struct {
	ID          string  `json:"id"`
	Description string  `json:"description"`
	Amount      float64 `json:"amount"`
}

func createExternalFund(t *testing.T, token, description string, amount float64) externalFundDTO {
	t.Helper()
	status, resp := doRequest(t, http.MethodPost, "/api/external-funds/", map[string]any{"description": description, "amount": amount}, token)
	if status != http.StatusCreated {
		t.Fatalf("create external fund fixture: expected 201, got %d (resp=%+v)", status, resp)
	}
	var f externalFundDTO
	decodeData(t, resp, &f)
	return f
}

func TestExternalFunds_RequireAuth(t *testing.T) {
	resetDB(t)

	cases := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/external-funds/"},
		{http.MethodPost, "/api/external-funds/"},
		{http.MethodGet, "/api/external-funds/" + nonexistentUUID},
		{http.MethodPut, "/api/external-funds/" + nonexistentUUID},
		{http.MethodDelete, "/api/external-funds/" + nonexistentUUID},
	}
	for _, c := range cases {
		status, _ := doRequest(t, c.method, c.path, nil, "")
		if status != http.StatusUnauthorized {
			t.Errorf("%s %s without token: expected 401, got %d", c.method, c.path, status)
		}
	}
}

func TestExternalFunds_NonSuperAdminForbidden(t *testing.T) {
	resetDB(t)
	kasir := seedUser(t, "KASIR", true)
	kasirToken := login(t, kasir.Email, kasir.Password)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	for _, tc := range []struct {
		role  string
		token string
	}{{"KASIR", kasirToken}, {"ADMIN", adminToken}} {
		status, resp := doRequest(t, http.MethodGet, "/api/external-funds/", nil, tc.token)
		if status != http.StatusForbidden {
			t.Errorf("list as %s: expected 403, got %d (resp=%+v)", tc.role, status, resp)
		}
		status, resp = doRequest(t, http.MethodPost, "/api/external-funds/", map[string]any{"description": "X", "amount": 1}, tc.token)
		if status != http.StatusForbidden {
			t.Errorf("create as %s: expected 403, got %d (resp=%+v)", tc.role, status, resp)
		}
	}
}

func TestExternalFunds_CreateListGetUpdateDelete(t *testing.T) {
	resetDB(t)
	superAdmin := seedUser(t, "SUPER_ADMIN", true)
	token := login(t, superAdmin.Email, superAdmin.Password)

	// The nominal Rupiah amount is what's tracked — gram weight is just
	// informal text inside the description (client requirement).
	status, resp := doRequest(t, http.MethodPost, "/api/external-funds/", map[string]any{"description": "Eliza Buyback 2 gram", "amount": 5000000}, token)
	if status != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d (resp=%+v)", status, resp)
	}
	var created externalFundDTO
	decodeData(t, resp, &created)
	if created.ID == "" || created.Description != "Eliza Buyback 2 gram" || created.Amount != 5000000 {
		t.Fatalf("create: unexpected response %+v", created)
	}

	status, resp = doRequest(t, http.MethodGet, "/api/external-funds/", nil, token)
	if status != http.StatusOK {
		t.Fatalf("list: expected 200, got %d", status)
	}
	var list []externalFundDTO
	decodeData(t, resp, &list)
	found := false
	for _, f := range list {
		if f.ID == created.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("list: expected created fund %s in list", created.ID)
	}

	status, resp = doRequest(t, http.MethodGet, "/api/external-funds/"+created.ID, nil, token)
	if status != http.StatusOK {
		t.Fatalf("get: expected 200, got %d", status)
	}

	status, resp = doRequest(t, http.MethodPut, "/api/external-funds/"+created.ID, map[string]any{"description": "Eliza Buyback 2 gram (lunas sebagian)", "amount": 3000000}, token)
	if status != http.StatusOK {
		t.Fatalf("update: expected 200, got %d (resp=%+v)", status, resp)
	}
	var updated externalFundDTO
	decodeData(t, resp, &updated)
	if updated.Amount != 3000000 {
		t.Fatalf("update: amount not applied, got %+v", updated)
	}

	// Delete is a real hard delete — no status field, no history; an entry
	// is removed outright once the money is settled (client requirement).
	status, resp = doRequest(t, http.MethodDelete, "/api/external-funds/"+created.ID, nil, token)
	if status != http.StatusOK {
		t.Fatalf("delete: expected 200, got %d (resp=%+v)", status, resp)
	}

	status, resp = doRequest(t, http.MethodGet, "/api/external-funds/"+created.ID, nil, token)
	if status != http.StatusNotFound {
		t.Fatalf("get after delete: expected 404 (hard delete), got %d (resp=%+v)", status, resp)
	}
}

func TestExternalFunds_CreateEmptyDescriptionBadRequest(t *testing.T) {
	resetDB(t)
	superAdmin := seedUser(t, "SUPER_ADMIN", true)
	token := login(t, superAdmin.Email, superAdmin.Password)

	status, resp := doRequest(t, http.MethodPost, "/api/external-funds/", map[string]any{"description": "", "amount": 1000000}, token)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (resp=%+v)", status, resp)
	}
}

func TestExternalFunds_CreateZeroOrNegativeAmountBadRequest(t *testing.T) {
	resetDB(t)
	superAdmin := seedUser(t, "SUPER_ADMIN", true)
	token := login(t, superAdmin.Email, superAdmin.Password)

	for _, amount := range []float64{0, -100} {
		status, resp := doRequest(t, http.MethodPost, "/api/external-funds/", map[string]any{"description": "Test", "amount": amount}, token)
		if status != http.StatusBadRequest {
			t.Errorf("amount=%v: expected 400, got %d (resp=%+v)", amount, status, resp)
		}
	}
}

func TestExternalFunds_GetNotFound(t *testing.T) {
	resetDB(t)
	superAdmin := seedUser(t, "SUPER_ADMIN", true)
	token := login(t, superAdmin.Email, superAdmin.Password)

	status, resp := doRequest(t, http.MethodGet, "/api/external-funds/"+nonexistentUUID, nil, token)
	if status != http.StatusNotFound {
		t.Fatalf("expected 404, got %d (resp=%+v)", status, resp)
	}
}

func TestExternalFunds_DeleteNotFound(t *testing.T) {
	resetDB(t)
	superAdmin := seedUser(t, "SUPER_ADMIN", true)
	token := login(t, superAdmin.Email, superAdmin.Password)

	status, resp := doRequest(t, http.MethodDelete, "/api/external-funds/"+nonexistentUUID, nil, token)
	if status != http.StatusNotFound {
		t.Fatalf("expected 404, got %d (resp=%+v)", status, resp)
	}
}
