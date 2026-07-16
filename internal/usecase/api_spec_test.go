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
func TestAPISpecPublishValidatesAndPreventsDuplicates(t *testing.T) {
	repo := &apiSpecRepo{}
	service := NewAPISpecService(repo)
	input := PublishAPISpecInput{SourceFilename: "openapi.yaml", Format: "yaml", Content: "openapi: 3.1.0\ninfo: {title: Test, version: v1}\npaths: {}\n"}
	first, unchanged, err := service.Publish(context.Background(), "prj_test", input)
	if err != nil || unchanged || first.Number != 1 {
		t.Fatalf("first publish = %#v unchanged=%v err=%v", first, unchanged, err)
	}
	second, unchanged, err := service.Publish(context.Background(), "prj_test", input)
	if err != nil || !unchanged || second.ID != first.ID {
		t.Fatalf("duplicate publish = %#v unchanged=%v err=%v", second, unchanged, err)
	}
}
func TestAPISpecRejectsNonOpenAPI(t *testing.T) {
	_, _, err := NewAPISpecService(&apiSpecRepo{}).Publish(context.Background(), "prj_test", PublishAPISpecInput{SourceFilename: "x.yaml", Format: "yaml", Content: "title: not-openapi"})
	if err == nil {
		t.Fatal("expected OpenAPI validation error")
	}
}
