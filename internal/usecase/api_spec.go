package usecase

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
	"kingdom_manager/backend/internal/domain/entity"
	"kingdom_manager/backend/internal/domain/port"
	"kingdom_manager/backend/internal/openapi"
)

type APISpecService struct {
	repo      port.APISpecRepository
	publisher Publisher
	workspace *Service
	now       func() time.Time
}

func NewAPISpecService(repo port.APISpecRepository, publisher Publisher, workspace *Service) *APISpecService {
	return &APISpecService{repo: repo, publisher: publisher, workspace: workspace, now: func() time.Time { return time.Now().UTC() }}
}

type PublishAPISpecInput struct{ SourceFilename, Format, Content, ContentHash, Message, GitBranch, GitCommitSHA, TokenID string }

// WorkspaceSyncSummary reports how a published revision was applied to the
// project's workspace resources. Applying is best-effort: the revision itself
// is already stored, so a failure here surfaces as Error instead of failing
// the publish.
type WorkspaceSyncSummary struct {
	Applied bool
	Created int
	Updated int
	Removed int
	Error   string
}

// PublishedAPISpec is the outcome of a CLI `sync`: the stored (or matching
// existing) revision plus how it landed in the workspace.
type PublishedAPISpec struct {
	Revision  *entity.APISpecRevision
	Unchanged bool
	Workspace *WorkspaceSyncSummary
}

// specSyncActor attributes workspace activity for CLI-published revisions —
// there is no collaborator behind an API key.
const specSyncActor = "Live Workspace CLI"

func (s *APISpecService) Publish(ctx context.Context, projectID string, in PublishAPISpecInput) (*PublishedAPISpec, error) {
	return s.publish(ctx, projectID, in, false)
}

// Checkout re-publishes a historical revision's exact content as a NEW
// current revision (history stays append-only; nothing is re-pointed or
// mutated) and applies it to the workspace with pruning, so spec-managed
// endpoints that do not exist in that version disappear from the web app too.
// Checking out the revision whose content already is current reports
// unchanged.
func (s *APISpecService) Checkout(ctx context.Context, projectID, revisionID, tokenID string) (*PublishedAPISpec, error) {
	target, err := s.Get(ctx, projectID, revisionID)
	if err != nil {
		return nil, err
	}
	return s.publish(ctx, projectID, PublishAPISpecInput{
		SourceFilename: target.SourceFilename, Format: target.Format, Content: target.Content,
		Message: fmt.Sprintf("Checkout of %s (#%d)", target.ID, target.Number), TokenID: tokenID,
	}, true)
}

func (s *APISpecService) publish(ctx context.Context, projectID string, in PublishAPISpecInput, prune bool) (*PublishedAPISpec, error) {
	if projectID == "" || in.SourceFilename == "" || (in.Format != "yaml" && in.Format != "json") {
		return nil, validation("project, source filename, and yaml/json format are required", nil)
	}
	if err := validateOpenAPI(in.Content, in.Format); err != nil {
		return nil, validation(err.Error(), nil)
	}
	hash := "sha256:" + hashContent(in.Content)
	if in.ContentHash != "" && in.ContentHash != hash {
		return nil, validation("content hash does not match uploaded content", nil)
	}
	revision := &entity.APISpecRevision{ID: "rev_" + shortID(), ProjectID: projectID, Status: "current", SourceFilename: in.SourceFilename, Format: in.Format, Content: in.Content, ContentHash: hash, Message: in.Message, GitBranch: in.GitBranch, GitCommitSHA: in.GitCommitSHA, CreatedByTokenID: in.TokenID, CreatedAt: s.now()}
	stored, unchanged, err := s.repo.Publish(ctx, revision)
	if err != nil {
		return nil, fmt.Errorf("publish api spec: %w", err)
	}
	out := &PublishedAPISpec{Revision: stored, Unchanged: unchanged}
	// API keys are minted per workspace (projectID == workspaceID), so the
	// project's web clients live on that same workspace stream.
	if !unchanged && s.publisher != nil {
		s.publisher.Publish(Event{Type: "api_spec.published", WorkspaceID: projectID, Payload: stored})
	}
	// A new revision also lands in the workspace resources so the web app
	// shows the synced endpoints through its regular resource flow.
	if !unchanged && s.workspace != nil {
		out.Workspace = s.applyToWorkspace(ctx, projectID, stored, prune)
	}
	return out, nil
}

func (s *APISpecService) applyToWorkspace(ctx context.Context, projectID string, revision *entity.APISpecRevision, prune bool) *WorkspaceSyncSummary {
	endpoints, err := openapi.Endpoints(revision.Content, revision.Format)
	if err != nil {
		return &WorkspaceSyncSummary{Error: err.Error()}
	}
	inputs := make([]ImportEndpointInput, len(endpoints))
	for i, endpoint := range endpoints {
		fields := make([]ImportFieldInput, len(endpoint.Fields))
		for j, field := range endpoint.Fields {
			fields[j] = ImportFieldInput{Key: field.Key, Type: field.Type, Required: field.Required, Description: field.Description, Value: field.Value}
		}
		responses := make([]ResponseSchemaInput, len(endpoint.Responses))
		for j, response := range endpoint.Responses {
			responseFields := make([]ResponseFieldInput, len(response.Fields))
			for k, field := range response.Fields {
				responseFields[k] = ResponseFieldInput{Key: field.Key, Type: field.Type, Required: field.Required, Description: field.Description, Value: field.Value}
			}
			responses[j] = ResponseSchemaInput{Status: response.Status, Description: response.Description, Fields: responseFields}
		}
		inputs[i] = ImportEndpointInput{Name: endpoint.Name, Method: endpoint.Method, Path: endpoint.Path, Fields: fields, Responses: responses}
	}
	result, err := s.workspace.ForWorkspace(projectID).ApplySpecEndpoints(ctx, specSyncActor, inputs, prune)
	if err != nil {
		return &WorkspaceSyncSummary{Error: err.Error()}
	}
	return &WorkspaceSyncSummary{Applied: true, Created: result.Created, Updated: result.Updated, Removed: result.Removed}
}
func (s *APISpecService) Current(ctx context.Context, projectID string) (*entity.APISpecRevision, error) {
	value, err := s.repo.Current(ctx, projectID)
	if err == port.ErrAPISpecNotFound {
		return nil, notFound("api specification", projectID)
	}
	return value, err
}
func (s *APISpecService) Get(ctx context.Context, projectID, id string) (*entity.APISpecRevision, error) {
	value, err := s.repo.Get(ctx, projectID, id)
	if err == port.ErrAPISpecNotFound {
		return nil, notFound("api specification revision", id)
	}
	return value, err
}
func (s *APISpecService) List(ctx context.Context, projectID string) ([]entity.APISpecRevision, error) {
	return s.repo.List(ctx, projectID)
}
func hashContent(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}
func validateOpenAPI(content, format string) error {
	var value map[string]any
	var err error
	if format == "json" {
		err = json.Unmarshal([]byte(content), &value)
	} else {
		err = yaml.Unmarshal([]byte(content), &value)
	}
	if err != nil {
		return fmt.Errorf("invalid OpenAPI %s", strings.ToUpper(format))
	}
	version, ok := value["openapi"].(string)
	if !ok || !strings.HasPrefix(version, "3.") {
		return fmt.Errorf("only OpenAPI 3.x documents are supported")
	}
	return nil
}
