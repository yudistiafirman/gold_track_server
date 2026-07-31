package e2e

import (
	"net/http"
	"testing"
)

type productDTO struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	SKU         string  `json:"sku"`
	CategoryID  string  `json:"category_id"`
	BrandID     string  `json:"brand_id"`
	WeightGram  float64 `json:"weight_gram"`
	Description string  `json:"description"`
	IsActive    bool    `json:"is_active"`
}

// createCategory/createBrand create a fixture via the real, already-tested
// endpoints rather than seeding SQL directly, so product tests exercise the
// full public_id -> internal-id resolution path.
func createCategory(t *testing.T, adminToken, name string) categoryDTO {
	t.Helper()
	status, resp := doRequest(t, http.MethodPost, "/api/categories/", map[string]string{"name": name}, adminToken)
	if status != http.StatusCreated {
		t.Fatalf("create category fixture: expected 201, got %d (resp=%+v)", status, resp)
	}
	var c categoryDTO
	decodeData(t, resp, &c)
	return c
}

func createBrand(t *testing.T, adminToken, name string) brandDTO {
	t.Helper()
	status, resp := doRequest(t, http.MethodPost, "/api/brands/", map[string]string{"name": name}, adminToken)
	if status != http.StatusCreated {
		t.Fatalf("create brand fixture: expected 201, got %d (resp=%+v)", status, resp)
	}
	var b brandDTO
	decodeData(t, resp, &b)
	return b
}

func TestProducts_RequireAuth(t *testing.T) {
	resetDB(t)

	status, _ := doRequest(t, http.MethodPost, "/api/products", nil, "")
	if status != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", status)
	}
}

func TestProducts_NonAdminForbidden(t *testing.T) {
	resetDB(t)
	kasir := seedUser(t, "KASIR", true)
	token := login(t, kasir.Email, kasir.Password)

	status, resp := doRequest(t, http.MethodPost, "/api/products", nil, token)
	if status != http.StatusForbidden {
		t.Fatalf("expected 403, got %d (resp=%+v)", status, resp)
	}
}

func TestProducts_CreateGeneratesSKU(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	category := createCategory(t, adminToken, "Batangan")
	brand := createBrand(t, adminToken, "Antam")

	status, resp := doRequest(t, http.MethodPost, "/api/products", map[string]any{
		"name":        "Emas Batangan 10gr",
		"category_id": category.ID,
		"brand_id":    brand.ID,
		"weight_gram": 10,
		"description": "Emas batangan Antam 10 gram",
	}, adminToken)
	if status != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d (resp=%+v)", status, resp)
	}

	var created productDTO
	decodeData(t, resp, &created)
	if created.ID == "" {
		t.Fatal("create: expected non-empty public id")
	}
	if created.SKU != "BAT-ANT-10-001" {
		t.Fatalf("create: expected sku BAT-ANT-10-001, got %q", created.SKU)
	}
	if !created.IsActive {
		t.Fatal("create: expected is_active=true by default")
	}
	if created.CategoryID != category.ID || created.BrandID != brand.ID {
		t.Fatalf("create: expected category_id/brand_id to echo request, got %+v", created)
	}
}

func TestProducts_SKUIncrementsForSameCombination(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	category := createCategory(t, adminToken, "Koin")
	brand := createBrand(t, adminToken, "UBS")

	body := map[string]any{
		"name":        "Koin Emas UBS 5gr",
		"category_id": category.ID,
		"brand_id":    brand.ID,
		"weight_gram": 5,
	}

	status, resp := doRequest(t, http.MethodPost, "/api/products", body, adminToken)
	if status != http.StatusCreated {
		t.Fatalf("first create: expected 201, got %d (resp=%+v)", status, resp)
	}
	var first productDTO
	decodeData(t, resp, &first)
	if first.SKU != "KOI-UBS-5-001" {
		t.Fatalf("first create: expected sku KOI-UBS-5-001, got %q", first.SKU)
	}

	status, resp = doRequest(t, http.MethodPost, "/api/products", body, adminToken)
	if status != http.StatusCreated {
		t.Fatalf("second create: expected 201, got %d (resp=%+v)", status, resp)
	}
	var second productDTO
	decodeData(t, resp, &second)
	if second.SKU != "KOI-UBS-5-002" {
		t.Fatalf("second create: expected sku KOI-UBS-5-002, got %q", second.SKU)
	}
}

func TestProducts_MissingRequiredFields(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	status, resp := doRequest(t, http.MethodPost, "/api/products", map[string]any{}, adminToken)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (resp=%+v)", status, resp)
	}
}

func TestProducts_InvalidWeight(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	category := createCategory(t, adminToken, "Perhiasan")
	brand := createBrand(t, adminToken, "Lotus")

	status, resp := doRequest(t, http.MethodPost, "/api/products", map[string]any{
		"name":        "Cincin Emas",
		"category_id": category.ID,
		"brand_id":    brand.ID,
		"weight_gram": 0,
	}, adminToken)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400 for zero weight, got %d (resp=%+v)", status, resp)
	}
}

func TestProducts_InvalidCategoryOrBrandID(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	brand := createBrand(t, adminToken, "Galeri24")

	status, resp := doRequest(t, http.MethodPost, "/api/products", map[string]any{
		"name":        "Produk Tanpa Kategori",
		"category_id": "00000000-0000-0000-0000-000000000000",
		"brand_id":    brand.ID,
		"weight_gram": 1,
	}, adminToken)
	if status != http.StatusNotFound {
		t.Fatalf("expected 404 for nonexistent category, got %d (resp=%+v)", status, resp)
	}
}

func TestProducts_InactiveCategoryRejected(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	category := createCategory(t, adminToken, "Galeri")
	brand := createBrand(t, adminToken, "Semar")

	status, resp := doRequest(t, http.MethodDelete, "/api/categories/"+category.ID, nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("deactivate category: expected 200, got %d (resp=%+v)", status, resp)
	}

	status, resp = doRequest(t, http.MethodPost, "/api/products", map[string]any{
		"name":        "Produk Kategori Nonaktif",
		"category_id": category.ID,
		"brand_id":    brand.ID,
		"weight_gram": 1,
	}, adminToken)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400 for inactive category, got %d (resp=%+v)", status, resp)
	}
}
