package model

import "time"

type User struct {
	ID           int64
	Name         string
	Email        string
	PasswordHash string
	Role         string
	IsActive     bool
	LastLoginAt  *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}
