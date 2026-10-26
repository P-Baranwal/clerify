package model

import "github.com/google/uuid"

// NewID returns a random UUID v4, matching crypto.randomUUID().
func NewID() string { return uuid.NewString() }
