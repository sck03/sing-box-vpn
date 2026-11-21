package domain

import "errors"

// ErrAlreadyExists is an expected creation conflict, not a storage outage.
var ErrAlreadyExists = errors.New("resource ID already exists")
