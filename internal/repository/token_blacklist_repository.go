package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type TokenBlacklistRepository interface {
	// Add blacklists jti until expiresAt (the token's own expiry — no need to
	// keep the row after that, the token would be rejected on expiry anyway).
	Add(ctx context.Context, jti string, expiresAt time.Time) error
	IsBlacklisted(ctx context.Context, jti string) (bool, error)
}

type tokenBlacklistRepository struct {
	db *pgxpool.Pool
}

func NewTokenBlacklistRepository(db *pgxpool.Pool) TokenBlacklistRepository {
	return &tokenBlacklistRepository{db: db}
}

func (r *tokenBlacklistRepository) Add(ctx context.Context, jti string, expiresAt time.Time) error {
	const query = `
		INSERT INTO token_blacklist (jti, expires_at) VALUES ($1, $2)
		ON CONFLICT (jti) DO NOTHING
	`
	if _, err := r.db.Exec(ctx, query, jti, expiresAt); err != nil {
		return fmt.Errorf("blacklist token: %w", err)
	}
	return nil
}

func (r *tokenBlacklistRepository) IsBlacklisted(ctx context.Context, jti string) (bool, error) {
	const query = `SELECT EXISTS (SELECT 1 FROM token_blacklist WHERE jti = $1)`
	var exists bool
	if err := r.db.QueryRow(ctx, query, jti).Scan(&exists); err != nil {
		return false, fmt.Errorf("check token blacklist: %w", err)
	}
	return exists, nil
}
