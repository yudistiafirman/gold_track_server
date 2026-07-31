package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"gold-track-be/internal/model"
)

// ErrExpenseNotFound is returned when no expense matches the lookup.
var ErrExpenseNotFound = errors.New("expense not found")

const expenseWithCategoryColumns = `
	e.id, e.public_id::text, e.category_id, e.amount::float8, e.description, e.expense_date,
	e.created_by, e.created_at,
	ec.public_id::text, ec.name
`

const expenseWithCategoryFrom = `
	FROM expenses e
	JOIN expense_categories ec ON ec.id = e.category_id
`

// ExpenseWithCategory is an expense joined with its category's identity —
// list/detail views need the name, not just the internal FK.
type ExpenseWithCategory struct {
	model.Expense
	CategoryPublicID string
	CategoryName     string
}

// ExpenseFilter narrows ExpenseRepository.List — DateFrom/DateTo are each
// independently optional (inclusive bounds) to support open-ended ranges.
type ExpenseFilter struct {
	CategoryID  *int64
	DateFrom    *time.Time
	DateTo      *time.Time
	Page, Limit int
}

type ExpenseRepository interface {
	Create(ctx context.Context, e *model.Expense) (*model.Expense, error)
	FindByPublicID(ctx context.Context, publicID string) (*ExpenseWithCategory, error)
	List(ctx context.Context, filter ExpenseFilter) ([]ExpenseWithCategory, int, error)
	Update(ctx context.Context, e *model.Expense) error
	// Delete hard-deletes — no FK dependents reference expenses.id, so
	// unlike expense_categories' Delete, no in-use guard is needed here.
	Delete(ctx context.Context, publicID string) (bool, error)
}

type expenseRepository struct {
	db *pgxpool.Pool
}

func NewExpenseRepository(db *pgxpool.Pool) ExpenseRepository {
	return &expenseRepository{db: db}
}

func scanExpenseWithCategory(row pgx.Row, e *ExpenseWithCategory) error {
	return row.Scan(
		&e.ID, &e.PublicID, &e.CategoryID, &e.Amount, &e.Description, &e.ExpenseDate,
		&e.CreatedBy, &e.CreatedAt,
		&e.CategoryPublicID, &e.CategoryName,
	)
}

func (r *expenseRepository) Create(ctx context.Context, e *model.Expense) (*model.Expense, error) {
	const query = `
		INSERT INTO expenses (category_id, amount, description, expense_date, created_by)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, public_id::text, created_at
	`

	err := r.db.QueryRow(ctx, query, e.CategoryID, e.Amount, e.Description, e.ExpenseDate, e.CreatedBy).
		Scan(&e.ID, &e.PublicID, &e.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("create expense: %w", err)
	}
	return e, nil
}

func (r *expenseRepository) FindByPublicID(ctx context.Context, publicID string) (*ExpenseWithCategory, error) {
	query := `SELECT ` + expenseWithCategoryColumns + expenseWithCategoryFrom + `WHERE e.public_id = $1`

	var e ExpenseWithCategory
	if err := scanExpenseWithCategory(r.db.QueryRow(ctx, query, publicID), &e); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrExpenseNotFound
		}
		return nil, fmt.Errorf("find expense by public id: %w", err)
	}
	return &e, nil
}

func (r *expenseRepository) List(ctx context.Context, filter ExpenseFilter) ([]ExpenseWithCategory, int, error) {
	var conditions []string
	var args []any

	if filter.CategoryID != nil {
		args = append(args, *filter.CategoryID)
		conditions = append(conditions, fmt.Sprintf("e.category_id = $%d", len(args)))
	}
	if filter.DateFrom != nil {
		args = append(args, *filter.DateFrom)
		conditions = append(conditions, fmt.Sprintf("e.expense_date >= $%d", len(args)))
	}
	if filter.DateTo != nil {
		args = append(args, *filter.DateTo)
		conditions = append(conditions, fmt.Sprintf("e.expense_date <= $%d", len(args)))
	}
	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}

	var total int
	countQuery := `SELECT COUNT(*) FROM expenses e ` + where
	if err := r.db.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count expenses: %w", err)
	}

	limitArg := len(args) + 1
	offsetArg := len(args) + 2
	listArgs := append(append([]any{}, args...), filter.Limit, (filter.Page-1)*filter.Limit)
	listQuery := `SELECT ` + expenseWithCategoryColumns + expenseWithCategoryFrom + where +
		fmt.Sprintf(" ORDER BY e.expense_date DESC, e.id DESC LIMIT $%d OFFSET $%d", limitArg, offsetArg)

	rows, err := r.db.Query(ctx, listQuery, listArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("list expenses: %w", err)
	}
	defer rows.Close()

	var expenses []ExpenseWithCategory
	for rows.Next() {
		var e ExpenseWithCategory
		if err := scanExpenseWithCategory(rows, &e); err != nil {
			return nil, 0, fmt.Errorf("scan expense row: %w", err)
		}
		expenses = append(expenses, e)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("list expenses: %w", err)
	}
	return expenses, total, nil
}

func (r *expenseRepository) Update(ctx context.Context, e *model.Expense) error {
	const query = `
		UPDATE expenses
		SET category_id = $1, amount = $2, description = $3, expense_date = $4
		WHERE id = $5
	`

	tag, err := r.db.Exec(ctx, query, e.CategoryID, e.Amount, e.Description, e.ExpenseDate, e.ID)
	if err != nil {
		return fmt.Errorf("update expense: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrExpenseNotFound
	}
	return nil
}

func (r *expenseRepository) Delete(ctx context.Context, publicID string) (bool, error) {
	tag, err := r.db.Exec(ctx, `DELETE FROM expenses WHERE public_id = $1`, publicID)
	if err != nil {
		return false, fmt.Errorf("delete expense: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}
