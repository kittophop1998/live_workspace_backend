package port

import (
	"context"
	"errors"
	"kingdom_manager/backend/internal/domain/entity"
)

var ErrAPISpecNotFound = errors.New("api specification not found")

type APISpecRepository interface {
	Publish(context.Context, *entity.APISpecRevision) (*entity.APISpecRevision, bool, error)
	Current(context.Context, string) (*entity.APISpecRevision, error)
	Get(context.Context, string, string) (*entity.APISpecRevision, error)
	List(context.Context, string) ([]entity.APISpecRevision, error)
}
