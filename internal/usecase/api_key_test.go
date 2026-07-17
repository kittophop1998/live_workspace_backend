package usecase

import (
	"context"
	"kingdom_manager/backend/internal/domain/entity"
	"kingdom_manager/backend/internal/domain/port"
	"testing"
)

type apiKeyRepo struct{ values map[string]*entity.APIKey }

func (r *apiKeyRepo) Create(_ context.Context, v *entity.APIKey) error {
	if r.values == nil {
		r.values = map[string]*entity.APIKey{}
	}
	r.values[v.ID] = v
	return nil
}
func (r *apiKeyRepo) FindByHash(_ context.Context, hash string) (*entity.APIKey, error) {
	for _, v := range r.values {
		if v.SecretHash == hash {
			return v, nil
		}
	}
	return nil, port.ErrAPISpecNotFound
}
func (r *apiKeyRepo) List(context.Context, string) ([]entity.APIKey, error) { return nil, nil }
func (r *apiKeyRepo) Revoke(context.Context, string, string) error          { return nil }
func (r *apiKeyRepo) Touch(context.Context, string) error                   { return nil }
func TestAPIKeyIsOpaqueAndScopeRestricted(t *testing.T) {
	service := NewAPIKeyService(&apiKeyRepo{})
	key, secret, err := service.Create(context.Background(), "prj_test", "CI", "col_test", []string{"api-spec:read"}, nil)
	if err != nil || secret == "" || key.SecretHash == secret {
		t.Fatalf("create key failed: %#v %q %v", key, secret, err)
	}
	if _, err := service.Authenticate(context.Background(), secret, "api-spec:write"); err == nil {
		t.Fatal("read-only key must not publish")
	}
	if _, err := service.Authenticate(context.Background(), secret, "api-spec:read"); err != nil {
		t.Fatalf("read key rejected: %v", err)
	}
}
