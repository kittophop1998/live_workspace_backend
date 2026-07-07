package usecase

import (
	"context"
	"testing"
	"time"

	"kingdom_manager/backend/internal/domain/entity"
	"kingdom_manager/backend/internal/domain/port"
)

type fakeStoryRepo struct {
	stories map[string]*entity.Story
}

func newFakeStoryRepo() *fakeStoryRepo {
	return &fakeStoryRepo{stories: map[string]*entity.Story{}}
}

func (r *fakeStoryRepo) CreateStory(_ context.Context, story *entity.Story) error {
	copy := *story
	copy.Steps = append([]entity.StoryStep(nil), story.Steps...)
	r.stories[story.ID] = &copy
	return nil
}

func (r *fakeStoryRepo) ListStories(_ context.Context, workspaceID string) ([]entity.Story, error) {
	out := []entity.Story{}
	for _, story := range r.stories {
		if story.WorkspaceID == workspaceID {
			out = append(out, *story)
		}
	}
	return out, nil
}

func (r *fakeStoryRepo) GetStory(_ context.Context, id string) (*entity.Story, error) {
	if story, ok := r.stories[id]; ok {
		copy := *story
		copy.Steps = append([]entity.StoryStep(nil), story.Steps...)
		return &copy, nil
	}
	return nil, port.ErrStoryNotFound
}

func (r *fakeStoryRepo) UpdateStory(_ context.Context, story *entity.Story) error {
	if _, ok := r.stories[story.ID]; !ok {
		return port.ErrStoryNotFound
	}
	copy := *story
	copy.Steps = append([]entity.StoryStep(nil), story.Steps...)
	r.stories[story.ID] = &copy
	return nil
}

func (r *fakeStoryRepo) DeleteStory(_ context.Context, workspaceID, id string) (bool, error) {
	story, ok := r.stories[id]
	if !ok || story.WorkspaceID != workspaceID {
		return false, nil
	}
	delete(r.stories, id)
	return true, nil
}

func TestStoryCreateAndUpdateReplacesSteps(t *testing.T) {
	repo := newFakeStoryRepo()
	service := NewStoryService(repo)
	service.now = func() time.Time { return time.Date(2026, 7, 7, 8, 0, 0, 0, time.UTC) }

	story, err := service.Create(context.Background(), "ws1", "Ava", StoryInput{
		Name: "Checkout flow",
		Steps: []StoryStepInput{
			{Type: "section", Text: "Payment"},
			{Type: "endpoint", ResourceID: "res_checkout", Text: "submit order"},
		},
	})
	if err != nil {
		t.Fatalf("create story: %v", err)
	}
	if story.ID == "" || story.CreatedBy != "Ava" || story.UpdatedBy != "Ava" {
		t.Fatalf("unexpected story metadata: %+v", story)
	}
	if len(story.Steps) != 2 || story.Steps[0].ID == "" || story.Steps[1].ResourceID != "res_checkout" {
		t.Fatalf("unexpected steps: %+v", story.Steps)
	}

	updated, err := service.Update(context.Background(), "ws1", story.ID, "Noah", StoryUpdateInput{
		Name: ptr("Checkout v2"),
		Steps: &[]StoryStepInput{
			{ID: story.Steps[1].ID, Type: "endpoint", ResourceID: "res_checkout_v2", Text: "submit order"},
		},
	})
	if err != nil {
		t.Fatalf("update story: %v", err)
	}
	if updated.Name != "Checkout v2" || updated.UpdatedBy != "Noah" {
		t.Fatalf("unexpected update metadata: %+v", updated)
	}
	if len(updated.Steps) != 1 || updated.Steps[0].ID != story.Steps[1].ID || updated.Steps[0].ResourceID != "res_checkout_v2" {
		t.Fatalf("steps were not replaced as expected: %+v", updated.Steps)
	}
}

func TestStoryValidation(t *testing.T) {
	service := NewStoryService(newFakeStoryRepo())
	tests := []struct {
		name  string
		input StoryInput
	}{
		{name: "missing name", input: StoryInput{}},
		{name: "invalid step type", input: StoryInput{Name: "Story", Steps: []StoryStepInput{{Type: "bad"}}}},
		{name: "endpoint missing resource", input: StoryInput{Name: "Story", Steps: []StoryStepInput{{Type: "endpoint"}}}},
		{name: "note with resource", input: StoryInput{Name: "Story", Steps: []StoryStepInput{{Type: "note", ResourceID: "res_1"}}}},
		{name: "duplicate step id", input: StoryInput{Name: "Story", Steps: []StoryStepInput{{ID: "sst_1", Type: "note"}, {ID: "sst_1", Type: "section"}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := service.Create(context.Background(), "ws1", "Ava", tt.input); err == nil {
				t.Fatal("want validation error")
			}
		})
	}
}

func TestStoryWorkspaceScope(t *testing.T) {
	repo := newFakeStoryRepo()
	service := NewStoryService(repo)
	story, err := service.Create(context.Background(), "ws1", "Ava", StoryInput{Name: "Checkout"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := service.Get(context.Background(), "ws2", story.ID); err == nil {
		t.Fatal("want not found for another workspace")
	}
	if err := service.Delete(context.Background(), "ws2", story.ID); err == nil {
		t.Fatal("want not found deleting from another workspace")
	}
	if err := service.Delete(context.Background(), "ws1", story.ID); err != nil {
		t.Fatalf("delete own story: %v", err)
	}
}

func ptr(value string) *string {
	return &value
}
