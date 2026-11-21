package clients

import "errors"

var (
	ErrNotFound     = errors.New("client not found")
	ErrCUITTaken    = errors.New("client cuit already exists")
	ErrArchived     = errors.New("client is archived")
	ErrEmailTaken   = errors.New("contact email already exists for this client")
	ErrContactLimit = errors.New("client contact limit reached")
)
