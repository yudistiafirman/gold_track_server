package model

import "time"

type Setting struct {
	ID          int64  // internal PK, never exposed via API
	PublicID    string // UUID, the only identifier exposed to API clients
	Key         string
	Value       string
	Description *string
	UpdatedBy   int64
	UpdatedAt   time.Time
}
