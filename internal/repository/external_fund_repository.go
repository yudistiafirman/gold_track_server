package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"gold-track-be/internal/model"
)

// ErrExternalFundNotFound is returned when no external fund entry matches
// the lookup.
var ErrExternalFundNotFound = errors.New("external fund not found")

const externalFundColumns = `id, public_id::text, description, amount, created_at`

type ExternalFundRepository interface {
	Create(ctx context.Context, f *model.ExternalFund) (*model.ExternalFund, error)
	FindByPublicID(ctx context.Context, publicID string) (*model.ExternalFund, error)
	List(ctx context.Context) ([]model.ExternalFund, error)
	Update(ctx context.Context, f *model.ExternalFund) error
	// Delete hard-deletes — no history is kept; an entry is removed outright
	// once the money is settled (client requirement).
	Delete(ctx context.Context, publicID string) (bool, error)
}

type externalFundRepository struct {
	db *pgxpool.Pool
}

func NewExternalFundRepository(db *pgxpool.Pool) ExternalFundRepository {
	return &externalFundRepository{db: db}
}

func scanExternalFund(row pgx.Row, f *model.ExternalFund) error {
	return row.Scan(&f.ID, &f.PublicID, &f.Description, &f.Amount, &f.CreatedAt)
}

func (r *externalFundRepository) Create(ctx context.Context, f *model.ExternalFund) (*model.ExternalFund, error) {
	const query = `
		INSERT INTO external_funds (description, amount)
		VALUES ($1, $2)
		RETURNING id, public_id::text, created_at
	`

	err := r.db.QueryRow(ctx, query, f.Description, f.Amount).Scan(&f.ID, &f.PublicID, &f.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("create external fund: %w", err)
	}
	return f, nil
}

func (r *externalFundRepository) FindByPublicID(ctx context.Context, publicID string) (*model.ExternalFund, error) {
	query := `SELECT ` + externalFundColumns + ` FROM external_funds WHERE public_id = $1`

	var f model.ExternalFund
	if err := scanExternalFund(r.db.QueryRow(ctx, query, publicID), &f); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrExternalFundNotFound
		}
		return nil, fmt.Errorf("find external fund by public id: %w", err)
	}
	return &f, nil
}

func (r *externalFundRepository) List(ctx context.Context) ([]model.ExternalFund, error) {
	query := `SELECT ` + externalFundColumns + ` FROM external_funds ORDER BY id DESC`

	rows, err := r.db.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list external funds: %w", err)
	}
	defer rows.Close()

	var funds []model.ExternalFund
	for rows.Next() {
		var f model.ExternalFund
		if err := scanExternalFund(rows, &f); err != nil {
			return nil, fmt.Errorf("scan external fund row: %w", err)
		}
		funds = append(funds, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list external funds: %w", err)
	}
	return funds, nil
}

func (r *externalFundRepository) Update(ctx context.Context, f *model.ExternalFund) error {
	const query = `UPDATE external_funds SET description = $1, amount = $2 WHERE id = $3`

	tag, err := r.db.Exec(ctx, query, f.Description, f.Amount, f.ID)
	if err != nil {
		return fmt.Errorf("update external fund: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrExternalFundNotFound
	}
	return nil
}

func (r *externalFundRepository) Delete(ctx context.Context, publicID string) (bool, error) {
	tag, err := r.db.Exec(ctx, `DELETE FROM external_funds WHERE public_id = $1`, publicID)
	if err != nil {
		return false, fmt.Errorf("delete external fund: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}
