package port

import (
	"context"
	"kingdom_manager/backend/internal/domain/entity"
)

type APIKeyRepository interface {
	Create(context.Context, *entity.APIKey) error
	FindByHash(context.Context, string) (*entity.APIKey, error)
	List(context.Context, string) ([]entity.APIKey, error)
	Revoke(context.Context, string, string) error
	Touch(context.Context, string) error
}
