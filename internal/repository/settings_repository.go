package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// SettingsRepository reads key-value shop settings (shop_name,
// shop_address, shop_phone, ...) used for display purposes like receipts.
type SettingsRepository interface {
	// GetByKeys returns whatever settings rows exist among keys — missing
	// keys are simply absent from the map, not an error.
	GetByKeys(ctx context.Context, keys []string) (map[string]string, error)
}

type settingsRepository struct {
	db *pgxpool.Pool
}

func NewSettingsRepository(db *pgxpool.Pool) SettingsRepository {
	return &settingsRepository{db: db}
}

func (r *settingsRepository) GetByKeys(ctx context.Context, keys []string) (map[string]string, error) {
	rows, err := r.db.Query(ctx, `SELECT key, value FROM settings WHERE key = ANY($1)`, keys)
	if err != nil {
		return nil, fmt.Errorf("get settings by keys: %w", err)
	}
	defer rows.Close()

	result := make(map[string]string, len(keys))
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, fmt.Errorf("scan setting row: %w", err)
		}
		result[key] = value
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("get settings by keys: %w", err)
	}
	return result, nil
}
