package mongo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"kingdom_manager/backend/internal/domain/entity"
	"kingdom_manager/backend/internal/domain/port"
)

type StoryRepository struct {
	stories *mongo.Collection
}

func NewStoryRepository(database *mongo.Database) *StoryRepository {
	return &StoryRepository{stories: database.Collection("stories")}
}

func (r *StoryRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.stories.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "workspace_id", Value: 1}, {Key: "created_at", Value: -1}}})
	return err
}

type storyStepDoc struct {
	ID         string `bson:"id"`
	Type       string `bson:"type"`
	ResourceID string `bson:"resource_id"`
	Text       string `bson:"text"`
}

type storyDoc struct {
	ID          string         `bson:"_id"`
	WorkspaceID string         `bson:"workspace_id"`
	Name        string         `bson:"name"`
	Steps       []storyStepDoc `bson:"steps"`
	CreatedAt   time.Time      `bson:"created_at"`
	CreatedBy   string         `bson:"created_by"`
	UpdatedAt   time.Time      `bson:"updated_at"`
	UpdatedBy   string         `bson:"updated_by"`
}

func (r *StoryRepository) CreateStory(ctx context.Context, story *entity.Story) error {
	if _, err := r.stories.InsertOne(ctx, toStoryDoc(story)); err != nil {
		return fmt.Errorf("insert story: %w", err)
	}
	return nil
}

func (r *StoryRepository) ListStories(ctx context.Context, workspaceID string) ([]entity.Story, error) {
	cursor, err := r.stories.Find(ctx, bson.M{"workspace_id": workspaceID}, options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}}))
	if err != nil {
		return nil, fmt.Errorf("list stories: %w", err)
	}
	defer cursor.Close(ctx)
	var docs []storyDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, fmt.Errorf("decode stories: %w", err)
	}
	out := make([]entity.Story, 0, len(docs))
	for i := range docs {
		out = append(out, *toStoryEntity(&docs[i]))
	}
	return out, nil
}

func (r *StoryRepository) GetStory(ctx context.Context, id string) (*entity.Story, error) {
	var doc storyDoc
	if err := r.stories.FindOne(ctx, bson.M{"_id": id}).Decode(&doc); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, port.ErrStoryNotFound
		}
		return nil, fmt.Errorf("find story: %w", err)
	}
	return toStoryEntity(&doc), nil
}

func (r *StoryRepository) UpdateStory(ctx context.Context, story *entity.Story) error {
	result, err := r.stories.ReplaceOne(ctx, bson.M{"_id": story.ID, "workspace_id": story.WorkspaceID}, toStoryDoc(story))
	if err != nil {
		return fmt.Errorf("replace story: %w", err)
	}
	if result.MatchedCount == 0 {
		return port.ErrStoryNotFound
	}
	return nil
}

func (r *StoryRepository) DeleteStory(ctx context.Context, workspaceID, id string) (bool, error) {
	result, err := r.stories.DeleteOne(ctx, bson.M{"_id": id, "workspace_id": workspaceID})
	if err != nil {
		return false, fmt.Errorf("delete story: %w", err)
	}
	return result.DeletedCount > 0, nil
}

func toStoryDoc(story *entity.Story) storyDoc {
	doc := storyDoc{
		ID: story.ID, WorkspaceID: story.WorkspaceID, Name: story.Name,
		CreatedAt: story.CreatedAt, CreatedBy: story.CreatedBy,
		UpdatedAt: story.UpdatedAt, UpdatedBy: story.UpdatedBy,
		Steps: make([]storyStepDoc, 0, len(story.Steps)),
	}
	for _, step := range story.Steps {
		doc.Steps = append(doc.Steps, storyStepDoc{ID: step.ID, Type: string(step.Type), ResourceID: step.ResourceID, Text: step.Text})
	}
	return doc
}

func toStoryEntity(doc *storyDoc) *entity.Story {
	story := &entity.Story{
		ID: doc.ID, WorkspaceID: doc.WorkspaceID, Name: doc.Name,
		CreatedAt: doc.CreatedAt, CreatedBy: doc.CreatedBy,
		UpdatedAt: doc.UpdatedAt, UpdatedBy: doc.UpdatedBy,
		Steps: make([]entity.StoryStep, 0, len(doc.Steps)),
	}
	for _, step := range doc.Steps {
		story.Steps = append(story.Steps, entity.StoryStep{ID: step.ID, Type: entity.StoryStepType(step.Type), ResourceID: step.ResourceID, Text: step.Text})
	}
	return story
}
