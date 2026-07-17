package usecase

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"kingdom_manager/backend/internal/domain/entity"
	"kingdom_manager/backend/internal/domain/port"
)

type APIKeyService struct {
	repo port.APIKeyRepository
	now  func() time.Time
}

func NewAPIKeyService(repo port.APIKeyRepository) *APIKeyService {
	return &APIKeyService{repo: repo, now: func() time.Time { return time.Now().UTC() }}
}
func (s *APIKeyService) Create(ctx context.Context, projectID, name, creator string, scopes []string, expiresAt *time.Time) (*entity.APIKey, string, error) {
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(name) == "" || len(scopes) == 0 {
		return nil, "", validation("project, name, and at least one scope are required", nil)
	}
	for _, scope := range scopes {
		if scope != "api-spec:read" && scope != "api-spec:write" && scope != "api-spec:revision:read" {
			return nil, "", validation("unsupported API key scope", nil)
		}
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, "", fmt.Errorf("generate api key: %w", err)
	}
	secret := "lws_prj_" + hex.EncodeToString(raw)
	hash := hashAPIKey(secret)
	value := &entity.APIKey{ID: "key_" + shortID(), ProjectID: projectID, Prefix: secret[:16], SecretHash: hash, Name: name, Scopes: scopes, CreatedBy: creator, CreatedAt: s.now(), ExpiresAt: expiresAt}
	if err := s.repo.Create(ctx, value); err != nil {
		return nil, "", err
	}
	return value, secret, nil
}
func (s *APIKeyService) Authenticate(ctx context.Context, raw string, required ...string) (*entity.APIKey, error) {
	if !strings.HasPrefix(raw, "lws_prj_") {
		return nil, ErrForbidden
	}
	value, err := s.repo.FindByHash(ctx, hashAPIKey(raw))
	if err != nil {
		return nil, ErrForbidden
	}
	if value.RevokedAt != nil || (value.ExpiresAt != nil && !value.ExpiresAt.After(s.now())) {
		return nil, ErrForbidden
	}
	have := map[string]bool{}
	for _, scope := range value.Scopes {
		have[scope] = true
	}
	for _, scope := range required {
		if !have[scope] {
			return nil, ErrForbidden
		}
	}
	_ = s.repo.Touch(ctx, value.ID)
	return value, nil
}
func (s *APIKeyService) List(ctx context.Context, projectID string) ([]entity.APIKey, error) {
	return s.repo.List(ctx, projectID)
}
func (s *APIKeyService) Revoke(ctx context.Context, projectID, id string) error {
	return s.repo.Revoke(ctx, projectID, id)
}
func hashAPIKey(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
