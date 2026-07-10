package mongo

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"kingdom_manager/backend/internal/domain/entity"
)

const taskLogsCollection = "task_logs"

// TaskLogRepository is append-only and deliberately not part of the versioned
// workspace aggregate: entries are immutable backend work-updates, so they need
// no per-revision copies and no optimistic-concurrency handling.
type TaskLogRepository struct {
	entries *mongo.Collection
}

func NewTaskLogRepository(database *mongo.Database) *TaskLogRepository {
	return &TaskLogRepository{entries: database.Collection(taskLogsCollection)}
}

func (r *TaskLogRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.entries.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "workspace_id", Value: 1}, {Key: "at", Value: 1}},
	})
	if err != nil {
		return fmt.Errorf("create %s indexes: %w", taskLogsCollection, err)
	}
	return nil
}

type taskLogDocument struct {
	DocumentID  string    `bson:"_id"`
	WorkspaceID string    `bson:"workspace_id"`
	ID          string    `bson:"id"`
	AuthorID    string    `bson:"author_id"`
	Author      string    `bson:"author"`
	Role        string    `bson:"role"`
	Kind        string    `bson:"kind"`
	Body        string    `bson:"body"`
	ResourceID  string    `bson:"resource_id"`
	At          time.Time `bson:"at"`
}

func (r *TaskLogRepository) Append(ctx context.Context, workspaceID string, entry entity.TaskLog) error {
	_, err := r.entries.InsertOne(ctx, taskLogDocument{
		DocumentID: workspaceID + ":" + entry.ID, WorkspaceID: workspaceID,
		ID: entry.ID, AuthorID: entry.AuthorID, Author: entry.Author,
		Role: string(entry.Role), Kind: string(entry.Kind), Body: entry.Body,
		ResourceID: entry.ResourceID, At: entry.At,
	})
	if err != nil {
		return fmt.Errorf("insert task log: %w", err)
	}
	return nil
}

func (r *TaskLogRepository) List(ctx context.Context, workspaceID string, limit int) ([]entity.TaskLog, error) {
	cursor, err := r.entries.Find(
		ctx,
		bson.M{"workspace_id": workspaceID},
		options.Find().SetSort(bson.D{{Key: "at", Value: -1}}).SetLimit(int64(limit)),
	)
	if err != nil {
		return nil, fmt.Errorf("find task logs: %w", err)
	}
	defer cursor.Close(ctx)
	var documents []taskLogDocument
	if err := cursor.All(ctx, &documents); err != nil {
		return nil, fmt.Errorf("decode task logs: %w", err)
	}
	out := make([]entity.TaskLog, len(documents))
	for i, value := range documents {
		out[len(documents)-1-i] = entity.TaskLog{
			ID: value.ID, AuthorID: value.AuthorID, Author: value.Author,
			Role: entity.CollaboratorRole(value.Role), Kind: entity.TaskLogKind(value.Kind),
			Body: value.Body, ResourceID: value.ResourceID, At: value.At,
		}
	}
	return out, nil
}
