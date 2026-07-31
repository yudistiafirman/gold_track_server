package repository

import (
	"context"
	"errors"
	"fmt"

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

const createProductMaxAttempts = 5

type ProductRepository interface {
	// CreateWithGeneratedSKU counts existing products sharing skuPrefix,
	// appends a zero-padded sequence number, and inserts — retrying the
	// whole count+insert unit on a concurrent unique-violation race.
	CreateWithGeneratedSKU(ctx context.Context, p *model.Product, skuPrefix string) (*model.Product, error)
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
