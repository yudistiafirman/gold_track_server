package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"gold-track-be/internal/model"
)

// ErrExternalDebtNotFound is returned when no external debt entry matches
// the lookup.
var ErrExternalDebtNotFound = errors.New("external debt not found")

const externalDebtColumns = `id, public_id::text, debtor_name, amount, created_at`

type ExternalDebtRepository interface {
	Create(ctx context.Context, d *model.ExternalDebt) (*model.ExternalDebt, error)
	FindByPublicID(ctx context.Context, publicID string) (*model.ExternalDebt, error)
	List(ctx context.Context) ([]model.ExternalDebt, error)
	Update(ctx context.Context, d *model.ExternalDebt) error
	// Delete hard-deletes — no history is kept; an entry is removed outright
	// once the debt is fully paid off (client requirement). Partial
	// repayment ("cicilan") is handled by Update lowering the amount, not by
	// a separate payment log.
	Delete(ctx context.Context, publicID string) (bool, error)
}

type externalDebtRepository struct {
	db *pgxpool.Pool
}

func NewExternalDebtRepository(db *pgxpool.Pool) ExternalDebtRepository {
	return &externalDebtRepository{db: db}
}

func scanExternalDebt(row pgx.Row, d *model.ExternalDebt) error {
	return row.Scan(&d.ID, &d.PublicID, &d.DebtorName, &d.Amount, &d.CreatedAt)
}

func (r *externalDebtRepository) Create(ctx context.Context, d *model.ExternalDebt) (*model.ExternalDebt, error) {
	const query = `
		INSERT INTO external_debts (debtor_name, amount)
		VALUES ($1, $2)
		RETURNING id, public_id::text, created_at
	`

	err := r.db.QueryRow(ctx, query, d.DebtorName, d.Amount).Scan(&d.ID, &d.PublicID, &d.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("create external debt: %w", err)
	}
	return d, nil
}

func (r *externalDebtRepository) FindByPublicID(ctx context.Context, publicID string) (*model.ExternalDebt, error) {
	query := `SELECT ` + externalDebtColumns + ` FROM external_debts WHERE public_id = $1`

	var d model.ExternalDebt
	if err := scanExternalDebt(r.db.QueryRow(ctx, query, publicID), &d); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrExternalDebtNotFound
		}
		return nil, fmt.Errorf("find external debt by public id: %w", err)
	}
	return &d, nil
}

func (r *externalDebtRepository) List(ctx context.Context) ([]model.ExternalDebt, error) {
	query := `SELECT ` + externalDebtColumns + ` FROM external_debts ORDER BY id DESC`

	rows, err := r.db.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list external debts: %w", err)
	}
	defer rows.Close()

	var debts []model.ExternalDebt
	for rows.Next() {
		var d model.ExternalDebt
		if err := scanExternalDebt(rows, &d); err != nil {
			return nil, fmt.Errorf("scan external debt row: %w", err)
		}
		debts = append(debts, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list external debts: %w", err)
	}
	return debts, nil
}

func (r *externalDebtRepository) Update(ctx context.Context, d *model.ExternalDebt) error {
	const query = `UPDATE external_debts SET debtor_name = $1, amount = $2 WHERE id = $3`

	tag, err := r.db.Exec(ctx, query, d.DebtorName, d.Amount, d.ID)
	if err != nil {
		return fmt.Errorf("update external debt: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrExternalDebtNotFound
	}
	return nil
}

func (r *externalDebtRepository) Delete(ctx context.Context, publicID string) (bool, error) {
	tag, err := r.db.Exec(ctx, `DELETE FROM external_debts WHERE public_id = $1`, publicID)
	if err != nil {
		return false, fmt.Errorf("delete external debt: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}
