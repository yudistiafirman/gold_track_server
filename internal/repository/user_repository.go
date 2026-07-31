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

// ErrUserNotFound is returned when no active user matches the lookup. Callers
// should treat it the same as an invalid password to avoid leaking account existence.
var ErrUserNotFound = errors.New("user not found")

type UserRepository interface {
	FindActiveByEmail(ctx context.Context, email string) (*model.User, error)
	UpdateLastLogin(ctx context.Context, userID int64, at time.Time) error
}

type userRepository struct {
	db *pgxpool.Pool
}

func NewUserRepository(db *pgxpool.Pool) UserRepository {
	return &userRepository{db: db}
}

func (r *userRepository) FindActiveByEmail(ctx context.Context, email string) (*model.User, error) {
	const query = `
		SELECT id, name, email, password_hash, role, is_active, last_login_at, created_at, updated_at
		FROM users
		WHERE email = $1 AND is_active = true
	`

	var u model.User
	err := r.db.QueryRow(ctx, query, email).Scan(
		&u.ID, &u.Name, &u.Email, &u.PasswordHash, &u.Role, &u.IsActive,
		&u.LastLoginAt, &u.CreatedAt, &u.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("find user by email: %w", err)
	}
	return &u, nil
}

func (r *userRepository) UpdateLastLogin(ctx context.Context, userID int64, at time.Time) error {
	const query = `UPDATE users SET last_login_at = $1, updated_at = $1 WHERE id = $2`
	if _, err := r.db.Exec(ctx, query, at, userID); err != nil {
		return fmt.Errorf("update last login: %w", err)
	}
	return nil
}
