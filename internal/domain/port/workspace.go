package port

import (
	"context"
	"errors"

	"kingdom_manager/backend/internal/domain/entity"
)

var ErrRevisionConflict = errors.New("workspace revision conflict")

type WorkspaceRepository interface {
	Get(context.Context, string) (*entity.Workspace, error)
	CreateIfAbsent(context.Context, *entity.Workspace) error
	Save(context.Context, *entity.Workspace, int64) error
}
