package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"gold-track-be/internal/model"
)

// ErrBrandNotFound is returned when no brand matches the lookup.
var ErrBrandNotFound = errors.New("brand not found")

// ErrBrandNameTaken is returned when a create/update would violate the
// case-insensitive unique index on brands.name.
var ErrBrandNameTaken = errors.New("brand name already in use")

// brandColumns are cast public_id::text so every scan target can be a plain
// Go string, regardless of pgx's uuid type mapping.
const brandColumns = `id, public_id::text, name, is_active, created_at, updated_at`

type BrandRepository interface {
	Create(ctx context.Context, b *model.Brand) (*model.Brand, error)
	FindByPublicID(ctx context.Context, publicID string) (*model.Brand, error)
	List(ctx context.Context) ([]model.Brand, error)
	Update(ctx context.Context, b *model.Brand) error
	SetActive(ctx context.Context, publicID string, isActive bool) error
}

type brandRepository struct {
	db *pgxpool.Pool
}

func NewBrandRepository(db *pgxpool.Pool) BrandRepository {
	return &brandRepository{db: db}
}

func scanBrand(row pgx.Row, b *model.Brand) error {
	return row.Scan(&b.ID, &b.PublicID, &b.Name, &b.IsActive, &b.CreatedAt, &b.UpdatedAt)
}

func (r *brandRepository) Create(ctx context.Context, b *model.Brand) (*model.Brand, error) {
	const query = `
		INSERT INTO brands (name, is_active)
		VALUES ($1, $2)
		RETURNING id, public_id::text, created_at, updated_at
	`

	err := r.db.QueryRow(ctx, query, b.Name, b.IsActive).
		Scan(&b.ID, &b.PublicID, &b.CreatedAt, &b.UpdatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrBrandNameTaken
		}
		return nil, fmt.Errorf("create brand: %w", err)
	}
	return b, nil
}

func (r *brandRepository) FindByPublicID(ctx context.Context, publicID string) (*model.Brand, error) {
	query := `SELECT ` + brandColumns + ` FROM brands WHERE public_id = $1`

	var b model.Brand
	if err := scanBrand(r.db.QueryRow(ctx, query, publicID), &b); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrBrandNotFound
		}
		return nil, fmt.Errorf("find brand by public id: %w", err)
	}
	return &b, nil
}

func (r *brandRepository) List(ctx context.Context) ([]model.Brand, error) {
	query := `SELECT ` + brandColumns + ` FROM brands ORDER BY id`

	rows, err := r.db.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list brands: %w", err)
	}
	defer rows.Close()

	var brands []model.Brand
	for rows.Next() {
		var b model.Brand
		if err := scanBrand(rows, &b); err != nil {
			return nil, fmt.Errorf("scan brand row: %w", err)
		}
		brands = append(brands, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list brands: %w", err)
	}
	return brands, nil
}

func (r *brandRepository) Update(ctx context.Context, b *model.Brand) error {
	const query = `
		UPDATE brands
		SET name = $1, is_active = $2, updated_at = now()
		WHERE id = $3
		RETURNING updated_at
	`

	err := r.db.QueryRow(ctx, query, b.Name, b.IsActive, b.ID).Scan(&b.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrBrandNotFound
		}
		if isUniqueViolation(err) {
			return ErrBrandNameTaken
		}
		return fmt.Errorf("update brand: %w", err)
	}
	return nil
}

func (r *brandRepository) SetActive(ctx context.Context, publicID string, isActive bool) error {
	const query = `UPDATE brands SET is_active = $1, updated_at = now() WHERE public_id = $2`
	tag, err := r.db.Exec(ctx, query, isActive, publicID)
	if err != nil {
		return fmt.Errorf("set brand active status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrBrandNotFound
	}
	return nil
}
