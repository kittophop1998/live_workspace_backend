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
)

type APISpecService struct {
	repo port.APISpecRepository
	now  func() time.Time
}

func NewAPISpecService(repo port.APISpecRepository) *APISpecService {
	return &APISpecService{repo: repo, now: func() time.Time { return time.Now().UTC() }}
}

type PublishAPISpecInput struct{ SourceFilename, Format, Content, ContentHash, Message, GitBranch, GitCommitSHA, TokenID string }

func (s *APISpecService) Publish(ctx context.Context, projectID string, in PublishAPISpecInput) (*entity.APISpecRevision, bool, error) {
	if projectID == "" || in.SourceFilename == "" || (in.Format != "yaml" && in.Format != "json") {
		return nil, false, validation("project, source filename, and yaml/json format are required", nil)
	}
	if err := validateOpenAPI(in.Content, in.Format); err != nil {
		return nil, false, validation(err.Error(), nil)
	}
	hash := "sha256:" + hashContent(in.Content)
	if in.ContentHash != "" && in.ContentHash != hash {
		return nil, false, validation("content hash does not match uploaded content", nil)
	}
	revision := &entity.APISpecRevision{ID: "rev_" + shortID(), ProjectID: projectID, Status: "current", SourceFilename: in.SourceFilename, Format: in.Format, Content: in.Content, ContentHash: hash, Message: in.Message, GitBranch: in.GitBranch, GitCommitSHA: in.GitCommitSHA, CreatedByTokenID: in.TokenID, CreatedAt: s.now()}
	stored, unchanged, err := s.repo.Publish(ctx, revision)
	if err != nil {
		return nil, false, fmt.Errorf("publish api spec: %w", err)
	}
	return stored, unchanged, nil
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
