package port

import (
	"context"
	"errors"

	"kingdom_manager/backend/internal/domain/entity"
)

var ErrFlowNotFound = errors.New("flow not found")
var ErrFlowRunNotFound = errors.New("flow run not found")

// FlowRepository persists E2E workflows and their run results. Flows live in
// their own collections (not the rev-guarded workspace document) so runs never
// conflict with concurrent schema edits.
type FlowRepository interface {
	CreateFlow(context.Context, *entity.FlowDefinition) error
	ListFlows(ctx context.Context, workspaceID string) ([]entity.FlowDefinition, error)
	GetFlow(ctx context.Context, id string) (*entity.FlowDefinition, error)
	DeleteFlow(ctx context.Context, workspaceID, id string) (bool, error)
	SaveRun(context.Context, *entity.FlowRun) error
	ListRuns(ctx context.Context, flowID string) ([]entity.FlowRun, error)
	GetRun(ctx context.Context, runID string) (*entity.FlowRun, error)
}

// FlowParser converts an uploaded workflow document into domain definitions.
type FlowParser interface {
	Parse([]byte) ([]entity.FlowDefinition, error)
}

// HTTPRequest and HTTPResponse are transport-neutral values used by flow runs.
// The infrastructure executor owns the actual outbound network I/O.
type HTTPRequest struct {
	Method  string
	URL     string
	Headers map[string]string
	Body    []byte
}

type HTTPResponse struct {
	Status     int
	DurationMs int64
	Headers    map[string][]string
	Body       string
	BodySize   int
	Truncated  bool
}

type HTTPExecutor interface {
	Exec(context.Context, HTTPRequest) (HTTPResponse, error)
}
