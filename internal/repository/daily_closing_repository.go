package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"gold-track-be/internal/model"
)

// ErrDailyClosingNotFound is returned when no daily closing matches the
// lookup.
var ErrDailyClosingNotFound = errors.New("daily closing not found")

// ErrDailyClosingDateTaken is returned when a create would violate the
// unique constraint on daily_closings.closing_date — that day is already
// closed.
var ErrDailyClosingDateTaken = errors.New("daily closing date already recorded")

const dailyClosingColumns = `id, public_id::text, closing_date, total_balance::float8, total_gold_value::float8, total_saldo::float8, created_by, created_at`

type DailyClosingRepository interface {
	Create(ctx context.Context, closingDate time.Time, totalBalance, totalGoldValue float64, createdBy int64) (*model.DailyClosing, error)
	// FindLatestBefore returns the most recent closing strictly before the
	// given date, or nil (not an error) when no closing has ever been
	// recorded before it — the "no baseline yet" case.
	FindLatestBefore(ctx context.Context, date time.Time) (*model.DailyClosing, error)
	FindByPublicID(ctx context.Context, publicID string) (*model.DailyClosing, error)
	List(ctx context.Context, page, limit int) ([]model.DailyClosing, int, error)
}

type dailyClosingRepository struct {
	db *pgxpool.Pool
}

func NewDailyClosingRepository(db *pgxpool.Pool) DailyClosingRepository {
	return &dailyClosingRepository{db: db}
}

func scanDailyClosing(row pgx.Row, c *model.DailyClosing) error {
	return row.Scan(&c.ID, &c.PublicID, &c.ClosingDate, &c.TotalBalance, &c.TotalGoldValue, &c.TotalSaldo, &c.CreatedBy, &c.CreatedAt)
}

func (r *dailyClosingRepository) Create(ctx context.Context, closingDate time.Time, totalBalance, totalGoldValue float64, createdBy int64) (*model.DailyClosing, error) {
	const query = `
		INSERT INTO daily_closings (closing_date, total_balance, total_gold_value, total_saldo, created_by)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING ` + dailyClosingColumns

	var c model.DailyClosing
	totalSaldo := totalBalance + totalGoldValue
	err := scanDailyClosing(r.db.QueryRow(ctx, query, closingDate, totalBalance, totalGoldValue, totalSaldo, createdBy), &c)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrDailyClosingDateTaken
		}
		return nil, fmt.Errorf("create daily closing: %w", err)
	}
	return &c, nil
}

func (r *dailyClosingRepository) FindLatestBefore(ctx context.Context, date time.Time) (*model.DailyClosing, error) {
	query := `SELECT ` + dailyClosingColumns + ` FROM daily_closings WHERE closing_date < $1 ORDER BY closing_date DESC LIMIT 1`

	var c model.DailyClosing
	if err := scanDailyClosing(r.db.QueryRow(ctx, query, date), &c); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("find latest daily closing before date: %w", err)
	}
	return &c, nil
}

func (r *dailyClosingRepository) FindByPublicID(ctx context.Context, publicID string) (*model.DailyClosing, error) {
	query := `SELECT ` + dailyClosingColumns + ` FROM daily_closings WHERE public_id = $1`

	var c model.DailyClosing
	if err := scanDailyClosing(r.db.QueryRow(ctx, query, publicID), &c); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrDailyClosingNotFound
		}
		return nil, fmt.Errorf("find daily closing by public id: %w", err)
	}
	return &c, nil
}

func (r *dailyClosingRepository) List(ctx context.Context, page, limit int) ([]model.DailyClosing, int, error) {
	var total int
	if err := r.db.QueryRow(ctx, `SELECT COUNT(*) FROM daily_closings`).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count daily closings: %w", err)
	}

	query := `SELECT ` + dailyClosingColumns + ` FROM daily_closings ORDER BY closing_date DESC LIMIT $1 OFFSET $2`
	rows, err := r.db.Query(ctx, query, limit, (page-1)*limit)
	if err != nil {
		return nil, 0, fmt.Errorf("list daily closings: %w", err)
	}
	defer rows.Close()

	var closings []model.DailyClosing
	for rows.Next() {
		var c model.DailyClosing
		if err := scanDailyClosing(rows, &c); err != nil {
			return nil, 0, fmt.Errorf("scan daily closing row: %w", err)
		}
		closings = append(closings, c)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("list daily closings: %w", err)
	}
	return closings, total, nil
}
