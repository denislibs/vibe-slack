package store

import "errors"

var (
	ErrNotFound = errors.New("store: not found")
	ErrConflict = errors.New("store: conflict")
)

// User is an account record.
type User struct {
	ID           string
	Email        string
	OpaqueRecord []byte
}
