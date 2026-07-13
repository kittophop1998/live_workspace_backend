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

type FeedbackRepository struct {
	feedback *mongo.Collection
}

func NewFeedbackRepository(database *mongo.Database) *FeedbackRepository {
	return &FeedbackRepository{feedback: database.Collection("feedback")}
}

func (r *FeedbackRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.feedback.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "workspace_id", Value: 1}, {Key: "created_at", Value: -1}}})
	return err
}

type feedbackDoc struct {
	ID          string    `bson:"_id"`
	WorkspaceID string    `bson:"workspace_id"`
	Category    string    `bson:"category"`
	Body        string    `bson:"body"`
	Author      string    `bson:"author"`
	AuthorRole  string    `bson:"author_role"`
	Status      string    `bson:"status"`
	CreatedAt   time.Time `bson:"created_at"`
	UpdatedAt   time.Time `bson:"updated_at"`
	UpdatedBy   string    `bson:"updated_by"`
}

func (r *FeedbackRepository) CreateFeedback(ctx context.Context, feedback *entity.Feedback) error {
	if _, err := r.feedback.InsertOne(ctx, toFeedbackDoc(feedback)); err != nil {
		return fmt.Errorf("insert feedback: %w", err)
	}
	return nil
}

func (r *FeedbackRepository) ListFeedback(ctx context.Context, workspaceID string) ([]entity.Feedback, error) {
	cursor, err := r.feedback.Find(ctx, bson.M{"workspace_id": workspaceID}, options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}}))
	if err != nil {
		return nil, fmt.Errorf("list feedback: %w", err)
	}
	defer cursor.Close(ctx)
	var docs []feedbackDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, fmt.Errorf("decode feedback: %w", err)
	}
	out := make([]entity.Feedback, 0, len(docs))
	for i := range docs {
		out = append(out, *toFeedbackEntity(&docs[i]))
	}
	return out, nil
}

func (r *FeedbackRepository) GetFeedback(ctx context.Context, id string) (*entity.Feedback, error) {
	var doc feedbackDoc
	if err := r.feedback.FindOne(ctx, bson.M{"_id": id}).Decode(&doc); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, port.ErrFeedbackNotFound
		}
		return nil, fmt.Errorf("find feedback: %w", err)
	}
	return toFeedbackEntity(&doc), nil
}

func (r *FeedbackRepository) UpdateFeedback(ctx context.Context, feedback *entity.Feedback) error {
	result, err := r.feedback.ReplaceOne(ctx, bson.M{"_id": feedback.ID, "workspace_id": feedback.WorkspaceID}, toFeedbackDoc(feedback))
	if err != nil {
		return fmt.Errorf("replace feedback: %w", err)
	}
	if result.MatchedCount == 0 {
		return port.ErrFeedbackNotFound
	}
	return nil
}

func (r *FeedbackRepository) DeleteFeedback(ctx context.Context, workspaceID, id string) (bool, error) {
	result, err := r.feedback.DeleteOne(ctx, bson.M{"_id": id, "workspace_id": workspaceID})
	if err != nil {
		return false, fmt.Errorf("delete feedback: %w", err)
	}
	return result.DeletedCount > 0, nil
}

func toFeedbackDoc(f *entity.Feedback) feedbackDoc {
	return feedbackDoc{
		ID: f.ID, WorkspaceID: f.WorkspaceID, Category: string(f.Category), Body: f.Body,
		Author: f.Author, AuthorRole: string(f.AuthorRole), Status: string(f.Status),
		CreatedAt: f.CreatedAt, UpdatedAt: f.UpdatedAt, UpdatedBy: f.UpdatedBy,
	}
}

func toFeedbackEntity(doc *feedbackDoc) *entity.Feedback {
	return &entity.Feedback{
		ID: doc.ID, WorkspaceID: doc.WorkspaceID, Category: entity.FeedbackCategory(doc.Category), Body: doc.Body,
		Author: doc.Author, AuthorRole: entity.CollaboratorRole(doc.AuthorRole), Status: entity.FeedbackStatus(doc.Status),
		CreatedAt: doc.CreatedAt, UpdatedAt: doc.UpdatedAt, UpdatedBy: doc.UpdatedBy,
	}
}
