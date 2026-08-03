package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"gold-track-be/internal/model"
)

const goldPriceColumns = `id, public_id::text, price_buy::float8, price_sell::float8, price_reference::float8, spread::float8, effective_date, effective_from, effective_until, is_active, source, notes, created_by, created_at`

// GoldPriceRepository persists the reference gold price (BE-404). Rows are
// never updated in place — ReplaceActive deactivates whatever row is
// currently active and inserts a new one atomically, so there's always
// exactly 0 or 1 active row.
type GoldPriceRepository interface {
	// GetActive returns the current is_active=true row, or (nil, nil) if
	// none exists yet (fresh deploy, sync job hasn't run yet).
	GetActive(ctx context.Context) (*model.GoldPrice, error)
	// ReplaceActive deactivates the current active row (if any) and
	// inserts price as the new active row, in a single transaction.
	ReplaceActive(ctx context.Context, price *model.GoldPrice) (*model.GoldPrice, error)
}

type goldPriceRepository struct {
	db *pgxpool.Pool
}

func NewGoldPriceRepository(db *pgxpool.Pool) GoldPriceRepository {
	return &goldPriceRepository{db: db}
}

func scanGoldPrice(row pgx.Row, p *model.GoldPrice) error {
	return row.Scan(
		&p.ID, &p.PublicID, &p.PriceBuy, &p.PriceSell, &p.PriceReference, &p.Spread,
		&p.EffectiveDate, &p.EffectiveFrom, &p.EffectiveUntil, &p.IsActive,
		&p.Source, &p.Notes, &p.CreatedBy, &p.CreatedAt,
	)
}

func (r *goldPriceRepository) GetActive(ctx context.Context) (*model.GoldPrice, error) {
	query := `SELECT ` + goldPriceColumns + ` FROM gold_prices WHERE is_active = true ORDER BY created_at DESC LIMIT 1`

	var p model.GoldPrice
	if err := scanGoldPrice(r.db.QueryRow(ctx, query), &p); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get active gold price: %w", err)
	}
	return &p, nil
}

func (r *goldPriceRepository) ReplaceActive(ctx context.Context, price *model.GoldPrice) (*model.GoldPrice, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `UPDATE gold_prices SET is_active = false, effective_until = now() WHERE is_active = true`); err != nil {
		return nil, fmt.Errorf("deactivate current gold price: %w", err)
	}

	const insertQuery = `
		INSERT INTO gold_prices (price_buy, price_sell, price_reference, spread, effective_date, effective_from, is_active, source, notes, created_by)
		VALUES ($1, $2, $3, $4, CURRENT_DATE, now(), true, $5, $6, $7)
		RETURNING id, public_id::text, effective_date, effective_from, is_active, created_at
	`
	err = tx.QueryRow(ctx, insertQuery,
		price.PriceBuy, price.PriceSell, price.PriceReference, price.Spread, price.Source, price.Notes, price.CreatedBy,
	).Scan(&price.ID, &price.PublicID, &price.EffectiveDate, &price.EffectiveFrom, &price.IsActive, &price.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("insert gold price: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}
	return price, nil
}
