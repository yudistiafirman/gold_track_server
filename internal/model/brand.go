package model

import "time"

type Brand struct {
	ID        int64  // internal PK, used for FKs/joins — never exposed via API
	PublicID  string // UUID, the only identifier exposed to API clients
	Name      string
	IsActive  bool
	CreatedAt time.Time
	UpdatedAt time.Time
}
