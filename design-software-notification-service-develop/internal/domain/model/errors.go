package model

import "errors"

// ErrNotFound is returned by the repository when no row matches the requested id.
var ErrNotFound = errors.New("notification not found")
