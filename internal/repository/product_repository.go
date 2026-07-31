package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"gold-track-be/internal/model"
)

// ErrSKUConflict signals a unique-violation race on products.sku between the
// COUNT-based urut computation and the INSERT — caller should recompute the
// count and retry. Never surfaces to the client.
var ErrSKUConflict = errors.New("sku conflict")

// ErrSKUGenerationFailed is returned once CreateWithGeneratedSKU exhausts its
// retry budget without landing a unique SKU.
var ErrSKUGenerationFailed = errors.New("failed to generate a unique sku")

// ErrProductNotFound is returned when no product matches the lookup.
var ErrProductNotFound = errors.New("product not found")

const createProductMaxAttempts = 5

// ProductFilter narrows ProductRepository.List. CategoryID/BrandID are
// resolved internal ids (nil means "no filter on this field") — the service
// layer resolves client-supplied public_ids before building this.
type ProductFilter struct {
	Search     string
	CategoryID *int64
	BrandID    *int64
	Page       int
	Limit      int
}

// ProductWithRefs is a product joined with its category/brand identity —
// list/detail views need the names, not just the internal FK, to render a
// human-facing catalog without a second round trip per row.
type ProductWithRefs struct {
	model.Product
	CategoryPublicID string
	CategoryName     string
	BrandPublicID    string
	BrandName        string
}

const productWithRefsColumns = `
	p.id, p.public_id::text, p.name, p.sku, p.category_id, p.brand_id,
	p.weight_gram::float8, p.description, p.is_active, p.created_by, p.created_at, p.updated_at,
	c.public_id::text, c.name, b.public_id::text, b.name
`

const productWithRefsFrom = `
	FROM products p
	JOIN categories c ON c.id = p.category_id
	JOIN brands b ON b.id = p.brand_id
`

type ProductRepository interface {
	// CreateWithGeneratedSKU counts existing products sharing skuPrefix,
	// appends a zero-padded sequence number, and inserts — retrying the
	// whole count+insert unit on a concurrent unique-violation race.
	CreateWithGeneratedSKU(ctx context.Context, p *model.Product, skuPrefix string) (*model.Product, error)
	// List returns active products (is_active = true) matching filter,
	// along with the total count ignoring pagination.
	List(ctx context.Context, filter ProductFilter) ([]ProductWithRefs, int, error)
	// FindByPublicID looks up a product regardless of is_active — archiving
	// only hides a product from List, it stays reachable by id.
	FindByPublicID(ctx context.Context, publicID string) (*ProductWithRefs, error)
	// Update applies name/category_id/brand_id/weight_gram/description/is_active
	// and bumps updated_at — sku is deliberately never in the SET clause, so
	// it cannot change regardless of what else is edited.
	Update(ctx context.Context, p *model.Product) error
	// HasAvailableStock reports whether any stock_items row for this
	// product still has status = 'AVAILABLE' — the guard for archiving.
	HasAvailableStock(ctx context.Context, productID int64) (bool, error)
	// SetActive flips is_active and bumps updated_at, identified by
	// public_id — used for archiving (is_active=false).
	SetActive(ctx context.Context, publicID string, isActive bool) error
}

type productRepository struct {
	db *pgxpool.Pool
}

func NewProductRepository(db *pgxpool.Pool) ProductRepository {
	return &productRepository{db: db}
}

func (r *productRepository) CreateWithGeneratedSKU(ctx context.Context, p *model.Product, skuPrefix string) (*model.Product, error) {
	for i := 0; i < createProductMaxAttempts; i++ {
		created, err := r.tryCreate(ctx, p, skuPrefix)
		if err == nil {
			return created, nil
		}
		if errors.Is(err, ErrSKUConflict) {
			continue
		}
		return nil, err
	}
	return nil, ErrSKUGenerationFailed
}

func (r *productRepository) tryCreate(ctx context.Context, p *model.Product, skuPrefix string) (*model.Product, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var count int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM products WHERE sku LIKE $1`, skuPrefix+"%").Scan(&count); err != nil {
		return nil, fmt.Errorf("count products by sku prefix: %w", err)
	}
	sku := fmt.Sprintf("%s%03d", skuPrefix, count+1)

	const insertQuery = `
		INSERT INTO products (name, sku, category_id, brand_id, weight_gram, description, is_active, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, public_id::text, created_at, updated_at
	`
	err = tx.QueryRow(ctx, insertQuery, p.Name, sku, p.CategoryID, p.BrandID, p.WeightGram, p.Description, p.IsActive, p.CreatedBy).
		Scan(&p.ID, &p.PublicID, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrSKUConflict
		}
		return nil, fmt.Errorf("insert product: %w", err)
	}
	p.SKU = sku

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}
	return p, nil
}

func scanProductWithRefs(row pgx.Row, p *ProductWithRefs) error {
	return row.Scan(
		&p.ID, &p.PublicID, &p.Name, &p.SKU, &p.CategoryID, &p.BrandID,
		&p.WeightGram, &p.Description, &p.IsActive, &p.CreatedBy, &p.CreatedAt, &p.UpdatedAt,
		&p.CategoryPublicID, &p.CategoryName, &p.BrandPublicID, &p.BrandName,
	)
}

func (r *productRepository) List(ctx context.Context, filter ProductFilter) ([]ProductWithRefs, int, error) {
	conditions := []string{"p.is_active = true"}
	var args []any

	if filter.Search != "" {
		args = append(args, "%"+filter.Search+"%")
		conditions = append(conditions, fmt.Sprintf("p.name ILIKE $%d", len(args)))
	}
	if filter.CategoryID != nil {
		args = append(args, *filter.CategoryID)
		conditions = append(conditions, fmt.Sprintf("p.category_id = $%d", len(args)))
	}
	if filter.BrandID != nil {
		args = append(args, *filter.BrandID)
		conditions = append(conditions, fmt.Sprintf("p.brand_id = $%d", len(args)))
	}
	where := "WHERE " + strings.Join(conditions, " AND ")

	var total int
	countQuery := `SELECT COUNT(*) FROM products p ` + where
	if err := r.db.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count products: %w", err)
	}

	limitArg := len(args) + 1
	offsetArg := len(args) + 2
	listArgs := append(append([]any{}, args...), filter.Limit, (filter.Page-1)*filter.Limit)
	listQuery := `SELECT ` + productWithRefsColumns + productWithRefsFrom + where +
		fmt.Sprintf(" ORDER BY p.id LIMIT $%d OFFSET $%d", limitArg, offsetArg)

	rows, err := r.db.Query(ctx, listQuery, listArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("list products: %w", err)
	}
	defer rows.Close()

	var products []ProductWithRefs
	for rows.Next() {
		var p ProductWithRefs
		if err := scanProductWithRefs(rows, &p); err != nil {
			return nil, 0, fmt.Errorf("scan product row: %w", err)
		}
		products = append(products, p)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("list products: %w", err)
	}
	return products, total, nil
}

func (r *productRepository) FindByPublicID(ctx context.Context, publicID string) (*ProductWithRefs, error) {
	query := `SELECT ` + productWithRefsColumns + productWithRefsFrom + `WHERE p.public_id = $1`

	var p ProductWithRefs
	if err := scanProductWithRefs(r.db.QueryRow(ctx, query, publicID), &p); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrProductNotFound
		}
		return nil, fmt.Errorf("find product by public id: %w", err)
	}
	return &p, nil
}

func (r *productRepository) Update(ctx context.Context, p *model.Product) error {
	const query = `
		UPDATE products
		SET name = $1, category_id = $2, brand_id = $3, weight_gram = $4,
			description = $5, is_active = $6, updated_at = now()
		WHERE id = $7
		RETURNING updated_at
	`
	err := r.db.QueryRow(ctx, query, p.Name, p.CategoryID, p.BrandID, p.WeightGram, p.Description, p.IsActive, p.ID).
		Scan(&p.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrProductNotFound
		}
		return fmt.Errorf("update product: %w", err)
	}
	return nil
}

func (r *productRepository) HasAvailableStock(ctx context.Context, productID int64) (bool, error) {
	const query = `SELECT EXISTS(SELECT 1 FROM stock_items WHERE product_id = $1 AND status = 'AVAILABLE')`
	var exists bool
	if err := r.db.QueryRow(ctx, query, productID).Scan(&exists); err != nil {
		return false, fmt.Errorf("check available stock: %w", err)
	}
	return exists, nil
}

func (r *productRepository) SetActive(ctx context.Context, publicID string, isActive bool) error {
	const query = `UPDATE products SET is_active = $1, updated_at = now() WHERE public_id = $2`
	tag, err := r.db.Exec(ctx, query, isActive, publicID)
	if err != nil {
		return fmt.Errorf("set product active status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrProductNotFound
	}
	return nil
}
