package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"gold-track-be/internal/model"
)

var ErrSettingNotFound = errors.New("setting not found")

// SettingsRepository reads/writes key-value shop settings (shop_name,
// shop_address, shop_phone, ...) used for display purposes like receipts.
// Rows are seeded up front (cmd/seed) — UpdateValue only ever updates an
// existing key, it never creates new ones.
type SettingsRepository interface {
	// GetByKeys returns whatever settings rows exist among keys — missing
	// keys are simply absent from the map, not an error.
	GetByKeys(ctx context.Context, keys []string) (map[string]string, error)
	// List returns the full rows among keys — missing keys are simply
	// absent from the result, not an error.
	List(ctx context.Context, keys []string) ([]model.Setting, error)
	// UpdateValue updates an existing key's value. Returns ErrSettingNotFound
	// if key doesn't already have a row.
	UpdateValue(ctx context.Context, key, value string, updatedBy int64) (*model.Setting, error)
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

func (r *settingsRepository) List(ctx context.Context, keys []string) ([]model.Setting, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, public_id::text, key, value, description, updated_by, updated_at
		FROM settings
		WHERE key = ANY($1)
		ORDER BY key
	`, keys)
	if err != nil {
		return nil, fmt.Errorf("list settings: %w", err)
	}
	defer rows.Close()

	var settings []model.Setting
	for rows.Next() {
		var s model.Setting
		if err := rows.Scan(&s.ID, &s.PublicID, &s.Key, &s.Value, &s.Description, &s.UpdatedBy, &s.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan setting row: %w", err)
		}
		settings = append(settings, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list settings: %w", err)
	}
	return settings, nil
}

func (r *settingsRepository) UpdateValue(ctx context.Context, key, value string, updatedBy int64) (*model.Setting, error) {
	var s model.Setting
	err := r.db.QueryRow(ctx, `
		UPDATE settings
		SET value = $1, updated_by = $2, updated_at = now()
		WHERE key = $3
		RETURNING id, public_id::text, key, value, description, updated_by, updated_at
	`, value, updatedBy, key).Scan(&s.ID, &s.PublicID, &s.Key, &s.Value, &s.Description, &s.UpdatedBy, &s.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrSettingNotFound
		}
		return nil, fmt.Errorf("update setting %q: %w", key, err)
	}
	return &s, nil
}
