package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"gold-track-be/internal/model"
)

// ErrBalanceAccountNotFound is returned when no balance account matches the
// lookup.
var ErrBalanceAccountNotFound = errors.New("balance account not found")

// ErrBalanceAccountNameTaken is returned when a create/update would violate
// the unique constraint on balance_accounts.name.
var ErrBalanceAccountNameTaken = errors.New("balance account name already in use")

const balanceAccountColumns = `id, public_id::text, name, balance, created_at`

type BalanceAccountRepository interface {
	Create(ctx context.Context, a *model.BalanceAccount) (*model.BalanceAccount, error)
	FindByPublicID(ctx context.Context, publicID string) (*model.BalanceAccount, error)
	List(ctx context.Context) ([]model.BalanceAccount, error)
	Update(ctx context.Context, a *model.BalanceAccount) error
	// Delete hard-deletes — no other table references balance_accounts via
	// FK, so unlike expense_categories there's no "in use" case to handle.
	Delete(ctx context.Context, publicID string) (bool, error)
}

type balanceAccountRepository struct {
	db *pgxpool.Pool
}

func NewBalanceAccountRepository(db *pgxpool.Pool) BalanceAccountRepository {
	return &balanceAccountRepository{db: db}
}

func scanBalanceAccount(row pgx.Row, a *model.BalanceAccount) error {
	return row.Scan(&a.ID, &a.PublicID, &a.Name, &a.Balance, &a.CreatedAt)
}

func (r *balanceAccountRepository) Create(ctx context.Context, a *model.BalanceAccount) (*model.BalanceAccount, error) {
	const query = `
		INSERT INTO balance_accounts (name, balance)
		VALUES ($1, $2)
		RETURNING id, public_id::text, created_at
	`

	err := r.db.QueryRow(ctx, query, a.Name, a.Balance).Scan(&a.ID, &a.PublicID, &a.CreatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrBalanceAccountNameTaken
		}
		return nil, fmt.Errorf("create balance account: %w", err)
	}
	return a, nil
}

func (r *balanceAccountRepository) FindByPublicID(ctx context.Context, publicID string) (*model.BalanceAccount, error) {
	query := `SELECT ` + balanceAccountColumns + ` FROM balance_accounts WHERE public_id = $1`

	var a model.BalanceAccount
	if err := scanBalanceAccount(r.db.QueryRow(ctx, query, publicID), &a); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrBalanceAccountNotFound
		}
		return nil, fmt.Errorf("find balance account by public id: %w", err)
	}
	return &a, nil
}

func (r *balanceAccountRepository) List(ctx context.Context) ([]model.BalanceAccount, error) {
	query := `SELECT ` + balanceAccountColumns + ` FROM balance_accounts ORDER BY id DESC`

	rows, err := r.db.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list balance accounts: %w", err)
	}
	defer rows.Close()

	var accounts []model.BalanceAccount
	for rows.Next() {
		var a model.BalanceAccount
		if err := scanBalanceAccount(rows, &a); err != nil {
			return nil, fmt.Errorf("scan balance account row: %w", err)
		}
		accounts = append(accounts, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list balance accounts: %w", err)
	}
	return accounts, nil
}

func (r *balanceAccountRepository) Update(ctx context.Context, a *model.BalanceAccount) error {
	const query = `UPDATE balance_accounts SET name = $1, balance = $2 WHERE id = $3`

	tag, err := r.db.Exec(ctx, query, a.Name, a.Balance, a.ID)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrBalanceAccountNameTaken
		}
		return fmt.Errorf("update balance account: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrBalanceAccountNotFound
	}
	return nil
}

func (r *balanceAccountRepository) Delete(ctx context.Context, publicID string) (bool, error) {
	tag, err := r.db.Exec(ctx, `DELETE FROM balance_accounts WHERE public_id = $1`, publicID)
	if err != nil {
		return false, fmt.Errorf("delete balance account: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}
