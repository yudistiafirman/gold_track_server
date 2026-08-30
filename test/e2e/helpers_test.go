package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type apiResponse struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data"`
	Error   *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// doRequest fires a real HTTP request at the httptest.Server wrapping the
// app's router and decodes the standard pkg/response envelope.
func doRequest(t *testing.T, method, path string, body any, token string) (int, apiResponse) {
	t.Helper()

	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
		reader = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, baseURL+path, reader)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}

	var parsed apiResponse
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &parsed) // some non-app responses (e.g. chi 404) aren't the JSON envelope
	}

	return resp.StatusCode, parsed
}

// decodeData unmarshals apiResponse.Data into dst, failing the test on error.
func decodeData(t *testing.T, resp apiResponse, dst any) {
	t.Helper()
	if err := json.Unmarshal(resp.Data, dst); err != nil {
		t.Fatalf("decode response data: %v (raw: %s)", err, resp.Data)
	}
}

// resetDB truncates every application table so each top-level test starts
// from a clean slate, independent of run order.
func resetDB(t *testing.T) {
	t.Helper()
	const query = `
		TRUNCATE TABLE
			token_blacklist, stock_opname_items, stock_opnames, expenses,
			expense_categories, transaction_items, transactions,
			purchase_order_items, purchase_orders, stock_items, gold_prices,
			balance_accounts, external_funds, external_debts, daily_closings,
			products, categories, brands, customers, suppliers, settings, users
		RESTART IDENTITY CASCADE
	`
	if _, err := testPool.Exec(context.Background(), query); err != nil {
		t.Fatalf("reset db: %v", err)
	}
}

type testUser struct {
	Email    string
	Password string
}

// seedUser inserts a user directly via SQL (bcrypt-hashed password), since
// there's no public signup endpoint — user creation itself is SUPER_ADMIN-only.
func seedUser(t *testing.T, role string, isActive bool) testUser {
	t.Helper()

	email := fmt.Sprintf("%s-%d@e2e.test", role, time.Now().UnixNano())
	password := "E2ePassword123"

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	_, err = testPool.Exec(context.Background(), `
		INSERT INTO users (name, email, password_hash, role, is_active)
		VALUES ($1, $2, $3, $4, $5)
	`, "E2E "+role, email, string(hash), role, isActive)
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}

	return testUser{Email: email, Password: password}
}

// login authenticates via the real /api/auth/login endpoint and returns the token.
func login(t *testing.T, email, password string) string {
	t.Helper()

	status, resp := doRequest(t, http.MethodPost, "/api/auth/login", map[string]string{
		"email":    email,
		"password": password,
	}, "")
	if status != http.StatusOK {
		t.Fatalf("login failed: status=%d resp=%+v", status, resp)
	}

	var data struct {
		Token string `json:"token"`
	}
	decodeData(t, resp, &data)
	return data.Token
}
