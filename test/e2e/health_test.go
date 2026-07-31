package e2e

import (
	"net/http"
	"testing"
)

func TestHealth(t *testing.T) {
	resetDB(t)

	status, resp := doRequest(t, http.MethodGet, "/health", nil, "")
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}
	if !resp.Success {
		t.Fatalf("expected success=true, got %+v", resp)
	}
}
