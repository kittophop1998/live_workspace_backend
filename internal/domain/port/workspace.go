package port

import (
	"context"
	"errors"

	"kingdom_manager/backend/internal/domain/entity"
)

var ErrRevisionConflict = errors.New("workspace revision conflict")
var ErrWorkspaceExists = errors.New("workspace already exists")
var ErrWorkspaceNotFound = errors.New("workspace not found")

type WorkspaceRepository interface {
	Get(context.Context, string) (*entity.Workspace, error)
	Create(context.Context, *entity.Workspace) error
	CreateIfAbsent(context.Context, *entity.Workspace) error
	Save(context.Context, *entity.Workspace, int64) error
}
