package usecase

import (
	"context"
	"kingdom_manager/backend/internal/domain/entity"
	"kingdom_manager/backend/internal/domain/port"
	"testing"
)

type apiSpecRepo struct {
	current *entity.APISpecRevision
	all     map[string]*entity.APISpecRevision
}

func (r *apiSpecRepo) Publish(_ context.Context, value *entity.APISpecRevision) (*entity.APISpecRevision, bool, error) {
	if r.all == nil {
		r.all = map[string]*entity.APISpecRevision{}
	}
	for _, v := range r.all {
		if v.ContentHash == value.ContentHash {
			return v, true, nil
		}
	}
	value.Number = int64(len(r.all) + 1)
	if r.current != nil {
		r.current.Status = "superseded"
		value.PreviousRevisionID = r.current.ID
	}
	r.all[value.ID] = value
	r.current = value
	return value, false, nil
}
func (r *apiSpecRepo) Current(context.Context, string) (*entity.APISpecRevision, error) {
	if r.current == nil {
		return nil, port.ErrAPISpecNotFound
	}
	return r.current, nil
}
func (r *apiSpecRepo) Get(_ context.Context, _ string, id string) (*entity.APISpecRevision, error) {
	v := r.all[id]
	if v == nil {
		return nil, port.ErrAPISpecNotFound
	}
	return v, nil
}
func (r *apiSpecRepo) List(context.Context, string) ([]entity.APISpecRevision, error) {
	out := []entity.APISpecRevision{}
	for _, v := range r.all {
		out = append(out, *v)
	}
	return out, nil
}
type capturePublisher struct{ events []Event }

func (p *capturePublisher) Publish(e Event) { p.events = append(p.events, e) }

func TestAPISpecPublishValidatesAndPreventsDuplicates(t *testing.T) {
	repo := &apiSpecRepo{}
	publisher := &capturePublisher{}
	service := NewAPISpecService(repo, publisher)
	input := PublishAPISpecInput{SourceFilename: "openapi.yaml", Format: "yaml", Content: "openapi: 3.1.0\ninfo: {title: Test, version: v1}\npaths: {}\n"}
	first, unchanged, err := service.Publish(context.Background(), "prj_test", input)
	if err != nil || unchanged || first.Number != 1 {
		t.Fatalf("first publish = %#v unchanged=%v err=%v", first, unchanged, err)
	}
	if len(publisher.events) != 1 || publisher.events[0].Type != "api_spec.published" || publisher.events[0].WorkspaceID != "prj_test" {
		t.Fatalf("expected one api_spec.published event for the workspace, got %#v", publisher.events)
	}
	second, unchanged, err := service.Publish(context.Background(), "prj_test", input)
	if err != nil || !unchanged || second.ID != first.ID {
		t.Fatalf("duplicate publish = %#v unchanged=%v err=%v", second, unchanged, err)
	}
	if len(publisher.events) != 1 {
		t.Fatalf("duplicate publish must not broadcast, got %d events", len(publisher.events))
	}
}
func TestAPISpecRejectsNonOpenAPI(t *testing.T) {
	_, _, err := NewAPISpecService(&apiSpecRepo{}, nil).Publish(context.Background(), "prj_test", PublishAPISpecInput{SourceFilename: "x.yaml", Format: "yaml", Content: "title: not-openapi"})
	if err == nil {
		t.Fatal("expected OpenAPI validation error")
	}
}
