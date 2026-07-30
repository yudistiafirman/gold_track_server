package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"gold-track-be/pkg/apperror"
)

type HealthRepository interface {
	Ping(ctx context.Context) error
}

type healthRepository struct {
	db *pgxpool.Pool
}

func NewHealthRepository(db *pgxpool.Pool) HealthRepository {
	return &healthRepository{db: db}
}

func (r *healthRepository) Ping(ctx context.Context) error {
	if err := r.db.Ping(ctx); err != nil {
		return apperror.Unavailable("database unreachable", fmt.Errorf("ping: %w", err))
	}
	return nil
}
