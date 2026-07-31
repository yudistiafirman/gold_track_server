package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"gold-track-be/internal/model"
)

// ErrUserNotFound is returned when no user matches the lookup. For login
// callers should treat it the same as an invalid password to avoid leaking
// account existence.
var ErrUserNotFound = errors.New("user not found")

// ErrEmailTaken is returned when a create/update would violate the unique
// constraint on users.email.
var ErrEmailTaken = errors.New("email already in use")

const uniqueViolationCode = "23505"

// userColumns are cast public_id::text so every scan target can be a plain
// Go string, regardless of pgx's uuid type mapping.
const userColumns = `id, public_id::text, name, email, password_hash, role, is_active, last_login_at, created_at, updated_at`

type UserRepository interface {
	FindActiveByEmail(ctx context.Context, email string) (*model.User, error)
	UpdateLastLogin(ctx context.Context, userID int64, at time.Time) error

	Create(ctx context.Context, u *model.User) (*model.User, error)
	FindByID(ctx context.Context, id int64) (*model.User, error)
	FindByPublicID(ctx context.Context, publicID string) (*model.User, error)
	List(ctx context.Context) ([]model.User, error)
	Update(ctx context.Context, u *model.User) error
	SetActive(ctx context.Context, publicID string, isActive bool) error
}

type userRepository struct {
	db *pgxpool.Pool
}

func NewUserRepository(db *pgxpool.Pool) UserRepository {
	return &userRepository{db: db}
}

func scanUser(row pgx.Row, u *model.User) error {
	return row.Scan(
		&u.ID, &u.PublicID, &u.Name, &u.Email, &u.PasswordHash, &u.Role, &u.IsActive,
		&u.LastLoginAt, &u.CreatedAt, &u.UpdatedAt,
	)
}

func (r *userRepository) FindActiveByEmail(ctx context.Context, email string) (*model.User, error) {
	query := `SELECT ` + userColumns + ` FROM users WHERE email = $1 AND is_active = true`

	var u model.User
	if err := scanUser(r.db.QueryRow(ctx, query, email), &u); err != nil {
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

func (r *userRepository) Create(ctx context.Context, u *model.User) (*model.User, error) {
	const query = `
		INSERT INTO users (name, email, password_hash, role, is_active)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, public_id::text, created_at, updated_at
	`

	err := r.db.QueryRow(ctx, query, u.Name, u.Email, u.PasswordHash, u.Role, u.IsActive).
		Scan(&u.ID, &u.PublicID, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrEmailTaken
		}
		return nil, fmt.Errorf("create user: %w", err)
	}
	return u, nil
}

func (r *userRepository) FindByID(ctx context.Context, id int64) (*model.User, error) {
	query := `SELECT ` + userColumns + ` FROM users WHERE id = $1`

	var u model.User
	if err := scanUser(r.db.QueryRow(ctx, query, id), &u); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("find user by id: %w", err)
	}
	return &u, nil
}

func (r *userRepository) FindByPublicID(ctx context.Context, publicID string) (*model.User, error) {
	query := `SELECT ` + userColumns + ` FROM users WHERE public_id = $1`

	var u model.User
	if err := scanUser(r.db.QueryRow(ctx, query, publicID), &u); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("find user by public id: %w", err)
	}
	return &u, nil
}

func (r *userRepository) List(ctx context.Context) ([]model.User, error) {
	query := `SELECT ` + userColumns + ` FROM users ORDER BY id`

	rows, err := r.db.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()

	var users []model.User
	for rows.Next() {
		var u model.User
		if err := scanUser(rows, &u); err != nil {
			return nil, fmt.Errorf("scan user row: %w", err)
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	return users, nil
}

func (r *userRepository) Update(ctx context.Context, u *model.User) error {
	const query = `
		UPDATE users
		SET name = $1, email = $2, password_hash = $3, role = $4, is_active = $5, updated_at = now()
		WHERE id = $6
		RETURNING updated_at
	`

	err := r.db.QueryRow(ctx, query, u.Name, u.Email, u.PasswordHash, u.Role, u.IsActive, u.ID).
		Scan(&u.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrUserNotFound
		}
		if isUniqueViolation(err) {
			return ErrEmailTaken
		}
		return fmt.Errorf("update user: %w", err)
	}
	return nil
}

func (r *userRepository) SetActive(ctx context.Context, publicID string, isActive bool) error {
	const query = `UPDATE users SET is_active = $1, updated_at = now() WHERE public_id = $2`
	tag, err := r.db.Exec(ctx, query, isActive, publicID)
	if err != nil {
		return fmt.Errorf("set user active status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrUserNotFound
	}
	return nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == uniqueViolationCode
}
