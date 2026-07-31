package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"gold-track-be/internal/model"
)

// ErrCustomerNotFound is returned when no customer matches the lookup.
var ErrCustomerNotFound = errors.New("customer not found")

// customerColumns are cast public_id::text so every scan target can be a
// plain Go string, regardless of pgx's uuid type mapping.
const customerColumns = `id, public_id::text, name, phone, email, id_type, id_number, address, notes, is_active, created_at, updated_at`

type CustomerFilter struct {
	Search string // ILIKE on name OR phone
	Page   int
	Limit  int
}

type CustomerRepository interface {
	Create(ctx context.Context, c *model.Customer) (*model.Customer, error)
	FindByPublicID(ctx context.Context, publicID string) (*model.Customer, error)
	List(ctx context.Context, filter CustomerFilter) ([]model.Customer, int, error)
	Update(ctx context.Context, c *model.Customer) error
	SetActive(ctx context.Context, publicID string, isActive bool) error
}

type customerRepository struct {
	db *pgxpool.Pool
}

func NewCustomerRepository(db *pgxpool.Pool) CustomerRepository {
	return &customerRepository{db: db}
}

func scanCustomer(row pgx.Row, c *model.Customer) error {
	return row.Scan(
		&c.ID, &c.PublicID, &c.Name, &c.Phone, &c.Email, &c.IDType, &c.IDNumber,
		&c.Address, &c.Notes, &c.IsActive, &c.CreatedAt, &c.UpdatedAt,
	)
}

func (r *customerRepository) Create(ctx context.Context, c *model.Customer) (*model.Customer, error) {
	const query = `
		INSERT INTO customers (name, phone, email, id_type, id_number, address, notes, is_active)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, public_id::text, created_at, updated_at
	`

	err := r.db.QueryRow(ctx, query, c.Name, c.Phone, c.Email, c.IDType, c.IDNumber, c.Address, c.Notes, c.IsActive).
		Scan(&c.ID, &c.PublicID, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("create customer: %w", err)
	}
	return c, nil
}

func (r *customerRepository) FindByPublicID(ctx context.Context, publicID string) (*model.Customer, error) {
	query := `SELECT ` + customerColumns + ` FROM customers WHERE public_id = $1`

	var c model.Customer
	if err := scanCustomer(r.db.QueryRow(ctx, query, publicID), &c); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrCustomerNotFound
		}
		return nil, fmt.Errorf("find customer by public id: %w", err)
	}
	return &c, nil
}

func (r *customerRepository) List(ctx context.Context, filter CustomerFilter) ([]model.Customer, int, error) {
	where := ""
	var args []any
	if filter.Search != "" {
		args = append(args, "%"+filter.Search+"%")
		where = "WHERE name ILIKE $1 OR phone ILIKE $1"
	}

	var total int
	countQuery := `SELECT COUNT(*) FROM customers ` + where
	if err := r.db.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count customers: %w", err)
	}

	limitArg := len(args) + 1
	offsetArg := len(args) + 2
	listArgs := append(append([]any{}, args...), filter.Limit, (filter.Page-1)*filter.Limit)
	listQuery := `SELECT ` + customerColumns + ` FROM customers ` + where +
		fmt.Sprintf(" ORDER BY id LIMIT $%d OFFSET $%d", limitArg, offsetArg)

	rows, err := r.db.Query(ctx, listQuery, listArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("list customers: %w", err)
	}
	defer rows.Close()

	var customers []model.Customer
	for rows.Next() {
		var c model.Customer
		if err := scanCustomer(rows, &c); err != nil {
			return nil, 0, fmt.Errorf("scan customer row: %w", err)
		}
		customers = append(customers, c)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("list customers: %w", err)
	}
	return customers, total, nil
}

func (r *customerRepository) Update(ctx context.Context, c *model.Customer) error {
	const query = `
		UPDATE customers
		SET name = $1, phone = $2, email = $3, id_type = $4, id_number = $5, address = $6,
			notes = $7, is_active = $8, updated_at = now()
		WHERE id = $9
		RETURNING updated_at
	`

	err := r.db.QueryRow(ctx, query, c.Name, c.Phone, c.Email, c.IDType, c.IDNumber, c.Address, c.Notes, c.IsActive, c.ID).
		Scan(&c.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrCustomerNotFound
		}
		return fmt.Errorf("update customer: %w", err)
	}
	return nil
}

func (r *customerRepository) SetActive(ctx context.Context, publicID string, isActive bool) error {
	const query = `UPDATE customers SET is_active = $1, updated_at = now() WHERE public_id = $2`
	tag, err := r.db.Exec(ctx, query, isActive, publicID)
	if err != nil {
		return fmt.Errorf("set customer active status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrCustomerNotFound
	}
	return nil
}
