package entity

import "time"

// APIKey stores only a one-way hash of the CLI secret.
type APIKey struct {
	ID, ProjectID, Prefix, SecretHash, Name, CreatedBy string
	Scopes                                             []string
	CreatedAt                                          time.Time
	ExpiresAt                                          *time.Time
	RevokedAt                                          *time.Time
	LastUsedAt                                         *time.Time
}
