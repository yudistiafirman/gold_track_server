package e2e

import (
	"context"
	"fmt"
	"net/http"
	"testing"
)

type productRefDTO struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type productDTO struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	SKU         string        `json:"sku"`
	Category    productRefDTO `json:"category"`
	Brand       productRefDTO `json:"brand"`
	WeightGram  float64       `json:"weight_gram"`
	Description string        `json:"description"`
	IsActive    bool          `json:"is_active"`
}

type paginationDTO struct {
	Page       int `json:"page"`
	Limit      int `json:"limit"`
	Total      int `json:"total"`
	TotalPages int `json:"total_pages"`
}

type productListDTO struct {
	Items      []productDTO  `json:"items"`
	Pagination paginationDTO `json:"pagination"`
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

func createProduct(t *testing.T, adminToken, name, categoryID, brandID string, weightGram float64) productDTO {
	t.Helper()
	status, resp := doRequest(t, http.MethodPost, "/api/products", map[string]any{
		"name":        name,
		"category_id": categoryID,
		"brand_id":    brandID,
		"weight_gram": weightGram,
	}, adminToken)
	if status != http.StatusCreated {
		t.Fatalf("create product fixture: expected 201, got %d (resp=%+v)", status, resp)
	}
	var p productDTO
	decodeData(t, resp, &p)
	return p
}

// deactivateProduct flips is_active directly via SQL — there's no product
// deactivate/delete endpoint in this ticket's scope, same reason seedUser
// bypasses the API for setup.
func deactivateProduct(t *testing.T, publicID string) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(), `UPDATE products SET is_active = false WHERE public_id = $1`, publicID); err != nil {
		t.Fatalf("deactivate product: %v", err)
	}
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
	if created.Category.ID != category.ID || created.Brand.ID != brand.ID {
		t.Fatalf("create: expected category/brand id to echo request, got %+v", created)
	}
	if created.Category.Name != "Batangan" || created.Brand.Name != "Antam" {
		t.Fatalf("create: expected category/brand name populated, got %+v", created)
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

func TestProducts_ListRequiresAuth(t *testing.T) {
	resetDB(t)

	status, _ := doRequest(t, http.MethodGet, "/api/products", nil, "")
	if status != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", status)
	}
}

func TestProducts_ListReturnsOnlyActiveWithPagination(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	category := createCategory(t, adminToken, "Batangan")
	brand := createBrand(t, adminToken, "Antam")

	createProduct(t, adminToken, "Produk Satu", category.ID, brand.ID, 10)
	createProduct(t, adminToken, "Produk Dua", category.ID, brand.ID, 20)
	archived := createProduct(t, adminToken, "Produk Arsip", category.ID, brand.ID, 30)
	deactivateProduct(t, archived.ID)

	status, resp := doRequest(t, http.MethodGet, "/api/products", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (resp=%+v)", status, resp)
	}
	var list productListDTO
	decodeData(t, resp, &list)
	if list.Pagination.Total != 2 {
		t.Fatalf("expected total=2 (archived excluded), got %d", list.Pagination.Total)
	}
	if len(list.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(list.Items))
	}
	for _, p := range list.Items {
		if p.ID == archived.ID {
			t.Fatal("archived product must not appear in active list")
		}
	}
}

func TestProducts_ListFiltersBySearchCategoryBrand(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	categoryA := createCategory(t, adminToken, "Batangan")
	brandX := createBrand(t, adminToken, "Antam")
	categoryB := createCategory(t, adminToken, "Perhiasan")
	brandY := createBrand(t, adminToken, "Lotus")

	p1 := createProduct(t, adminToken, "Emas Batangan 10gr", categoryA.ID, brandX.ID, 10)
	p2 := createProduct(t, adminToken, "Cincin Berlian", categoryB.ID, brandY.ID, 3)

	status, resp := doRequest(t, http.MethodGet, "/api/products?search=Batangan", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("search: expected 200, got %d (resp=%+v)", status, resp)
	}
	var bySearch productListDTO
	decodeData(t, resp, &bySearch)
	if len(bySearch.Items) != 1 || bySearch.Items[0].ID != p1.ID {
		t.Fatalf("search=Batangan: expected only p1, got %+v", bySearch.Items)
	}

	status, resp = doRequest(t, http.MethodGet, "/api/products?category_id="+categoryA.ID, nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("category filter: expected 200, got %d (resp=%+v)", status, resp)
	}
	var byCategory productListDTO
	decodeData(t, resp, &byCategory)
	if len(byCategory.Items) != 1 || byCategory.Items[0].ID != p1.ID {
		t.Fatalf("category_id filter: expected only p1, got %+v", byCategory.Items)
	}

	status, resp = doRequest(t, http.MethodGet, "/api/products?brand_id="+brandY.ID, nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("brand filter: expected 200, got %d (resp=%+v)", status, resp)
	}
	var byBrand productListDTO
	decodeData(t, resp, &byBrand)
	if len(byBrand.Items) != 1 || byBrand.Items[0].ID != p2.ID {
		t.Fatalf("brand_id filter: expected only p2, got %+v", byBrand.Items)
	}
}

func TestProducts_ListPaginationLimitsAndPages(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	category := createCategory(t, adminToken, "Koin")
	brand := createBrand(t, adminToken, "UBS")

	for i := 1; i <= 3; i++ {
		createProduct(t, adminToken, fmt.Sprintf("Koin Emas %d", i), category.ID, brand.ID, float64(i))
	}

	status, resp := doRequest(t, http.MethodGet, "/api/products?limit=2&page=1", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("page 1: expected 200, got %d (resp=%+v)", status, resp)
	}
	var page1 productListDTO
	decodeData(t, resp, &page1)
	if len(page1.Items) != 2 {
		t.Fatalf("page 1: expected 2 items, got %d", len(page1.Items))
	}
	if page1.Pagination.Total != 3 || page1.Pagination.TotalPages != 2 {
		t.Fatalf("page 1: expected total=3 total_pages=2, got %+v", page1.Pagination)
	}

	status, resp = doRequest(t, http.MethodGet, "/api/products?limit=2&page=2", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("page 2: expected 200, got %d (resp=%+v)", status, resp)
	}
	var page2 productListDTO
	decodeData(t, resp, &page2)
	if len(page2.Items) != 1 {
		t.Fatalf("page 2: expected 1 item, got %d", len(page2.Items))
	}
}

func TestProducts_ListUnknownCategoryFilterReturnsEmpty(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	status, resp := doRequest(t, http.MethodGet, "/api/products?category_id=00000000-0000-0000-0000-000000000000", nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (resp=%+v)", status, resp)
	}
	var list productListDTO
	decodeData(t, resp, &list)
	if len(list.Items) != 0 || list.Pagination.Total != 0 {
		t.Fatalf("expected empty result for unknown category filter, got %+v", list)
	}
}

func TestProducts_GetReturnsFullDetail(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	category := createCategory(t, adminToken, "Galeri")
	brand := createBrand(t, adminToken, "Semar")
	created := createProduct(t, adminToken, "Kalung Emas", category.ID, brand.ID, 15)

	status, resp := doRequest(t, http.MethodGet, "/api/products/"+created.ID, nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d (resp=%+v)", status, resp)
	}
	var fetched productDTO
	decodeData(t, resp, &fetched)
	if fetched.Name != "Kalung Emas" || fetched.SKU != created.SKU {
		t.Fatalf("expected matching name/sku, got %+v", fetched)
	}
	if fetched.Category.ID != category.ID || fetched.Category.Name != "Galeri" {
		t.Fatalf("expected category ref populated, got %+v", fetched.Category)
	}
	if fetched.Brand.ID != brand.ID || fetched.Brand.Name != "Semar" {
		t.Fatalf("expected brand ref populated, got %+v", fetched.Brand)
	}
}

func TestProducts_GetArchivedStillVisible(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	category := createCategory(t, adminToken, "Batangan")
	brand := createBrand(t, adminToken, "Antam")
	created := createProduct(t, adminToken, "Produk Arsip", category.ID, brand.ID, 10)
	deactivateProduct(t, created.ID)

	status, resp := doRequest(t, http.MethodGet, "/api/products/"+created.ID, nil, adminToken)
	if status != http.StatusOK {
		t.Fatalf("expected 200 for archived product detail, got %d (resp=%+v)", status, resp)
	}
	var fetched productDTO
	decodeData(t, resp, &fetched)
	if fetched.IsActive {
		t.Fatal("expected is_active=false on archived product detail")
	}
}

func TestProducts_GetNotFound(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	status, resp := doRequest(t, http.MethodGet, "/api/products/00000000-0000-0000-0000-000000000000", nil, adminToken)
	if status != http.StatusNotFound {
		t.Fatalf("expected 404, got %d (resp=%+v)", status, resp)
	}
}

func TestProducts_GetInvalidIDFormat(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)

	status, resp := doRequest(t, http.MethodGet, "/api/products/1", nil, adminToken)
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400 for non-UUID id, got %d (resp=%+v)", status, resp)
	}
}

func TestProducts_KasirCanListAndGet(t *testing.T) {
	resetDB(t)
	admin := seedUser(t, "ADMIN", true)
	adminToken := login(t, admin.Email, admin.Password)
	kasir := seedUser(t, "KASIR", true)
	kasirToken := login(t, kasir.Email, kasir.Password)

	category := createCategory(t, adminToken, "Batangan")
	brand := createBrand(t, adminToken, "Antam")
	created := createProduct(t, adminToken, "Produk Kasir", category.ID, brand.ID, 10)

	status, resp := doRequest(t, http.MethodGet, "/api/products", nil, kasirToken)
	if status != http.StatusOK {
		t.Fatalf("kasir list: expected 200, got %d (resp=%+v)", status, resp)
	}

	status, resp = doRequest(t, http.MethodGet, "/api/products/"+created.ID, nil, kasirToken)
	if status != http.StatusOK {
		t.Fatalf("kasir get: expected 200, got %d (resp=%+v)", status, resp)
	}
}
