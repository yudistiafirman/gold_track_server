package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"gold-track-be/internal/model"
)

// ErrCategoryNotFound is returned when no category matches the lookup.
var ErrCategoryNotFound = errors.New("category not found")

// ErrCategoryNameTaken is returned when a create/update would violate the
// case-insensitive unique index on categories.name.
var ErrCategoryNameTaken = errors.New("category name already in use")

// categoryColumns are cast public_id::text so every scan target can be a
// plain Go string, regardless of pgx's uuid type mapping.
const categoryColumns = `id, public_id::text, name, is_active, created_at, updated_at`

type CategoryRepository interface {
	Create(ctx context.Context, c *model.Category) (*model.Category, error)
	FindByPublicID(ctx context.Context, publicID string) (*model.Category, error)
	List(ctx context.Context) ([]model.Category, error)
	Update(ctx context.Context, c *model.Category) error
	SetActive(ctx context.Context, publicID string, isActive bool) error
}

type categoryRepository struct {
	db *pgxpool.Pool
}

func NewCategoryRepository(db *pgxpool.Pool) CategoryRepository {
	return &categoryRepository{db: db}
}

func scanCategory(row pgx.Row, c *model.Category) error {
	return row.Scan(&c.ID, &c.PublicID, &c.Name, &c.IsActive, &c.CreatedAt, &c.UpdatedAt)
}

func (r *categoryRepository) Create(ctx context.Context, c *model.Category) (*model.Category, error) {
	const query = `
		INSERT INTO categories (name, is_active)
		VALUES ($1, $2)
		RETURNING id, public_id::text, created_at, updated_at
	`

	err := r.db.QueryRow(ctx, query, c.Name, c.IsActive).
		Scan(&c.ID, &c.PublicID, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrCategoryNameTaken
		}
		return nil, fmt.Errorf("create category: %w", err)
	}
	return c, nil
}

func (r *categoryRepository) FindByPublicID(ctx context.Context, publicID string) (*model.Category, error) {
	query := `SELECT ` + categoryColumns + ` FROM categories WHERE public_id = $1`

	var c model.Category
	if err := scanCategory(r.db.QueryRow(ctx, query, publicID), &c); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrCategoryNotFound
		}
		return nil, fmt.Errorf("find category by public id: %w", err)
	}
	return &c, nil
}

func (r *categoryRepository) List(ctx context.Context) ([]model.Category, error) {
	query := `SELECT ` + categoryColumns + ` FROM categories ORDER BY id DESC`

	rows, err := r.db.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list categories: %w", err)
	}
	defer rows.Close()

	var categories []model.Category
	for rows.Next() {
		var c model.Category
		if err := scanCategory(rows, &c); err != nil {
			return nil, fmt.Errorf("scan category row: %w", err)
		}
		categories = append(categories, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list categories: %w", err)
	}
	return categories, nil
}

func (r *categoryRepository) Update(ctx context.Context, c *model.Category) error {
	const query = `
		UPDATE categories
		SET name = $1, is_active = $2, updated_at = now()
		WHERE id = $3
		RETURNING updated_at
	`

	err := r.db.QueryRow(ctx, query, c.Name, c.IsActive, c.ID).Scan(&c.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrCategoryNotFound
		}
		if isUniqueViolation(err) {
			return ErrCategoryNameTaken
		}
		return fmt.Errorf("update category: %w", err)
	}
	return nil
}

func (r *categoryRepository) SetActive(ctx context.Context, publicID string, isActive bool) error {
	const query = `UPDATE categories SET is_active = $1, updated_at = now() WHERE public_id = $2`
	tag, err := r.db.Exec(ctx, query, isActive, publicID)
	if err != nil {
		return fmt.Errorf("set category active status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrCategoryNotFound
	}
	return nil
}
