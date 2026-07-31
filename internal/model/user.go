package model

import "time"

type User struct {
	ID           int64  // internal PK, used for FKs/joins — never exposed via API
	PublicID     string // UUID, the only identifier exposed to API clients
	Name         string
	Email        string
	PasswordHash string
	Role         string
	IsActive     bool
	LastLoginAt  *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}
