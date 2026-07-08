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

const chatMessagesCollection = "chat_messages"

// ChatRepository is append-only and deliberately not part of the versioned
// workspace aggregate: messages are immutable, so they need no per-revision
// copies and no optimistic-concurrency handling.
type ChatRepository struct {
	messages *mongo.Collection
}

func NewChatRepository(database *mongo.Database) *ChatRepository {
	return &ChatRepository{messages: database.Collection(chatMessagesCollection)}
}

func (r *ChatRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.messages.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "workspace_id", Value: 1}, {Key: "at", Value: 1}},
	})
	if err != nil {
		return fmt.Errorf("create %s indexes: %w", chatMessagesCollection, err)
	}
	return nil
}

type chatMessageDocument struct {
	DocumentID  string    `bson:"_id"`
	WorkspaceID string    `bson:"workspace_id"`
	ID          string    `bson:"id"`
	AuthorID    string    `bson:"author_id"`
	Author      string    `bson:"author"`
	Role        string    `bson:"role"`
	Body        string    `bson:"body"`
	At          time.Time `bson:"at"`
}

func (r *ChatRepository) Append(ctx context.Context, workspaceID string, message entity.ChatMessage) error {
	_, err := r.messages.InsertOne(ctx, chatMessageDocument{
		DocumentID: workspaceID + ":" + message.ID, WorkspaceID: workspaceID,
		ID: message.ID, AuthorID: message.AuthorID, Author: message.Author,
		Role: string(message.Role), Body: message.Body, At: message.At,
	})
	if err != nil {
		return fmt.Errorf("insert chat message: %w", err)
	}
	return nil
}

func (r *ChatRepository) List(ctx context.Context, workspaceID string, limit int) ([]entity.ChatMessage, error) {
	cursor, err := r.messages.Find(
		ctx,
		bson.M{"workspace_id": workspaceID},
		options.Find().SetSort(bson.D{{Key: "at", Value: -1}}).SetLimit(int64(limit)),
	)
	if err != nil {
		return nil, fmt.Errorf("find chat messages: %w", err)
	}
	defer cursor.Close(ctx)
	var documents []chatMessageDocument
	if err := cursor.All(ctx, &documents); err != nil {
		return nil, fmt.Errorf("decode chat messages: %w", err)
	}
	out := make([]entity.ChatMessage, len(documents))
	for i, value := range documents {
		out[len(documents)-1-i] = entity.ChatMessage{
			ID: value.ID, AuthorID: value.AuthorID, Author: value.Author,
			Role: entity.CollaboratorRole(value.Role), Body: value.Body, At: value.At,
		}
	}
	return out, nil
}
