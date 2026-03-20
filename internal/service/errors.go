package service

import (
	"errors"

	"github.com/rulekit-dev/rulekit-registry/internal/port"
)

// Domain-level sentinel errors returned by all service methods.
// Callers should check these rather than importing port or adapter packages.
var (
	ErrNotFound         = errors.New("not found")
	ErrAlreadyExists    = errors.New("already exists")
	ErrVersionImmutable = errors.New("version already published and is immutable")
)

// mapErr translates outbound adapter errors into domain-level errors so that
// inbound adapters (HTTP handlers, etc.) never need to import port or adapter packages.
func mapErr(err error) error {
	switch {
	case errors.Is(err, port.ErrNotFound), errors.Is(err, port.ErrBlobNotFound):
		return ErrNotFound
	case errors.Is(err, port.ErrAlreadyExists):
		return ErrAlreadyExists
	case errors.Is(err, port.ErrVersionImmutable):
		return ErrVersionImmutable
	}
	return err
}
