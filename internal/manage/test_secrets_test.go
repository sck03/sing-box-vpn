package manage

import (
	"crypto/rand"
	"github.com/google/uuid"
)

func randomSecret() string { return rand.Text() }
func randomUUID() string   { return uuid.NewString() }
