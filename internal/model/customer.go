package model

import "time"

type Customer struct {
	ID        int64  // internal PK, used for FKs/joins — never exposed via API
	PublicID  string // UUID, the only identifier exposed to API clients
	Name      string
	Phone     *string
	Email     *string
	IDType    *string // KTP | SIM | PASSPORT, nil = not provided
	IDNumber  *string
	Address   *string
	Notes     *string
	IsActive  bool
	CreatedAt time.Time
	UpdatedAt time.Time
}
