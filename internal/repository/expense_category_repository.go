package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"gold-track-be/internal/model"
)

// ErrExpenseCategoryNotFound is returned when no expense category matches
// the lookup.
var ErrExpenseCategoryNotFound = errors.New("expense category not found")

// ErrExpenseCategoryNameTaken is returned when a create/update would
// violate the unique constraint on expense_categories.name.
var ErrExpenseCategoryNameTaken = errors.New("expense category name already in use")

// ErrExpenseCategoryInUse is returned when a delete is rejected because an
// expense still references this category.
var ErrExpenseCategoryInUse = errors.New("expense category still referenced by an expense")

const expenseCategoryColumns = `id, public_id::text, name, created_at`

type ExpenseCategoryRepository interface {
	Create(ctx context.Context, c *model.ExpenseCategory) (*model.ExpenseCategory, error)
	FindByPublicID(ctx context.Context, publicID string) (*model.ExpenseCategory, error)
	List(ctx context.Context) ([]model.ExpenseCategory, error)
	Update(ctx context.Context, c *model.ExpenseCategory) error
	// Delete hard-deletes — expense_categories has no is_active column,
	// unlike categories/brands/suppliers/customers. Returns whether a row
	// was actually removed so the caller can disambiguate "not found" from
	// "in use" (the latter surfaces as ErrExpenseCategoryInUse, not a
	// false return, since it's a DB-level error, not a no-op).
	Delete(ctx context.Context, publicID string) (bool, error)
}

type expenseCategoryRepository struct {
	db *pgxpool.Pool
}

func NewExpenseCategoryRepository(db *pgxpool.Pool) ExpenseCategoryRepository {
	return &expenseCategoryRepository{db: db}
}

func scanExpenseCategory(row pgx.Row, c *model.ExpenseCategory) error {
	return row.Scan(&c.ID, &c.PublicID, &c.Name, &c.CreatedAt)
}

func (r *expenseCategoryRepository) Create(ctx context.Context, c *model.ExpenseCategory) (*model.ExpenseCategory, error) {
	const query = `
		INSERT INTO expense_categories (name)
		VALUES ($1)
		RETURNING id, public_id::text, created_at
	`

	err := r.db.QueryRow(ctx, query, c.Name).Scan(&c.ID, &c.PublicID, &c.CreatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrExpenseCategoryNameTaken
		}
		return nil, fmt.Errorf("create expense category: %w", err)
	}
	return c, nil
}

func (r *expenseCategoryRepository) FindByPublicID(ctx context.Context, publicID string) (*model.ExpenseCategory, error) {
	query := `SELECT ` + expenseCategoryColumns + ` FROM expense_categories WHERE public_id = $1`

	var c model.ExpenseCategory
	if err := scanExpenseCategory(r.db.QueryRow(ctx, query, publicID), &c); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrExpenseCategoryNotFound
		}
		return nil, fmt.Errorf("find expense category by public id: %w", err)
	}
	return &c, nil
}

func (r *expenseCategoryRepository) List(ctx context.Context) ([]model.ExpenseCategory, error) {
	query := `SELECT ` + expenseCategoryColumns + ` FROM expense_categories ORDER BY id DESC`

	rows, err := r.db.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list expense categories: %w", err)
	}
	defer rows.Close()

	var categories []model.ExpenseCategory
	for rows.Next() {
		var c model.ExpenseCategory
		if err := scanExpenseCategory(rows, &c); err != nil {
			return nil, fmt.Errorf("scan expense category row: %w", err)
		}
		categories = append(categories, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list expense categories: %w", err)
	}
	return categories, nil
}

func (r *expenseCategoryRepository) Update(ctx context.Context, c *model.ExpenseCategory) error {
	const query = `UPDATE expense_categories SET name = $1 WHERE id = $2`

	tag, err := r.db.Exec(ctx, query, c.Name, c.ID)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrExpenseCategoryNameTaken
		}
		return fmt.Errorf("update expense category: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrExpenseCategoryNotFound
	}
	return nil
}

func (r *expenseCategoryRepository) Delete(ctx context.Context, publicID string) (bool, error) {
	tag, err := r.db.Exec(ctx, `DELETE FROM expense_categories WHERE public_id = $1`, publicID)
	if err != nil {
		if isForeignKeyViolation(err) {
			return false, ErrExpenseCategoryInUse
		}
		return false, fmt.Errorf("delete expense category: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}
