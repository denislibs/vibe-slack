package store

import "errors"

var (
	ErrNotFound      = errors.New("store: not found")
	ErrConflict      = errors.New("store: conflict")
	ErrUsernameTaken = errors.New("store: username taken")
	ErrSlugTaken     = errors.New("store: workspace slug taken")
)

// User is an account record.
type User struct {
	ID           string
	Email        string
	Username     string
	OpaqueRecord []byte
}

// Device is a registered device for a user.
type Device struct {
	ID     string
	UserID string
	Label  string
	Status string
}
