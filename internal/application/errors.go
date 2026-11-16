package application

import "errors"

var ErrConflict = errors.New("configuration changed; reload and preview again")
