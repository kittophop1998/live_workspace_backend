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

type WorkspaceRepository struct {
	collection *mongo.Collection
}

func NewWorkspaceRepository(database *mongo.Database) *WorkspaceRepository {
	return &WorkspaceRepository{collection: database.Collection("workspaces")}
}

type workspaceDocument struct {
	ID            string                 `bson:"_id"`
	Rev           int64                  `bson:"rev"`
	Resources     []resourceDocument     `bson:"resources"`
	Comments      []commentDocument      `bson:"comments"`
	Activity      []activityDocument     `bson:"activity"`
	Collaborators []collaboratorDocument `bson:"collaborators"`
}
type collaboratorDocument struct {
	ID, Name, Role, Color string
}
type fieldDocument struct {
	ID, Key, Type, State, Change string
	Required                     bool
	Description                  *string
	Value                        any
}
type resourceDocument struct {
	ID, Name, Kind, State, UpdatedBy string
	Method, Path                     *string
	Fields                           []fieldDocument
	UpdatedAt                        time.Time
}
type commentDocument struct {
	ID, ResourceID, AuthorID, Author, Role, Body string
	FieldID                                      *string
	At                                           time.Time
}
type activityDocument struct {
	ID, Actor, Verb, Target, ResourceID string
	At                                  time.Time
}

func (r *WorkspaceRepository) Get(ctx context.Context, id string) (*entity.Workspace, error) {
	var document workspaceDocument
	if err := r.collection.FindOne(ctx, bson.M{"_id": id}).Decode(&document); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, port.ErrWorkspaceNotFound
		}
		return nil, fmt.Errorf("find workspace: %w", err)
	}
	return toEntity(document), nil
}

func (r *WorkspaceRepository) CreateIfAbsent(ctx context.Context, workspace *entity.Workspace) error {
	_, err := r.collection.UpdateOne(ctx, bson.M{"_id": workspace.ID}, bson.M{"$setOnInsert": toDocument(workspace)}, options.Update().SetUpsert(true))
	if err != nil {
		return fmt.Errorf("seed workspace: %w", err)
	}
	return nil
}

func (r *WorkspaceRepository) Create(ctx context.Context, workspace *entity.Workspace) error {
	if _, err := r.collection.InsertOne(ctx, toDocument(workspace)); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return port.ErrWorkspaceExists
		}
		return fmt.Errorf("insert workspace: %w", err)
	}
	return nil
}

func (r *WorkspaceRepository) Save(ctx context.Context, workspace *entity.Workspace, expectedRev int64) error {
	result, err := r.collection.ReplaceOne(ctx, bson.M{"_id": workspace.ID, "rev": expectedRev}, toDocument(workspace))
	if err != nil {
		return fmt.Errorf("replace workspace: %w", err)
	}
	if result.MatchedCount == 0 {
		return port.ErrRevisionConflict
	}
	return nil
}

func (r *WorkspaceRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.collection.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "rev", Value: 1}}})
	return err
}

func toDocument(ws *entity.Workspace) workspaceDocument {
	doc := workspaceDocument{ID: ws.ID, Rev: ws.Rev}
	doc.Collaborators = make([]collaboratorDocument, len(ws.Collaborators))
	for i, value := range ws.Collaborators {
		doc.Collaborators[i] = collaboratorDocument{ID: value.ID, Name: value.Name, Role: string(value.Role), Color: value.Color}
	}
	doc.Resources = make([]resourceDocument, len(ws.Resources))
	for i, value := range ws.Resources {
		item := resourceDocument{ID: value.ID, Name: value.Name, Kind: string(value.Kind), Method: value.Method, Path: value.Path, State: string(value.State), UpdatedAt: value.UpdatedAt, UpdatedBy: value.UpdatedBy}
		item.Fields = make([]fieldDocument, len(value.Fields))
		for j, field := range value.Fields {
			item.Fields[j] = fieldDocument{ID: field.ID, Key: field.Key, Type: field.Type, Required: field.Required, State: string(field.State), Change: string(field.Change), Description: field.Description, Value: field.Value}
		}
		doc.Resources[i] = item
	}
	doc.Comments = make([]commentDocument, len(ws.Comments))
	for i, value := range ws.Comments {
		doc.Comments[i] = commentDocument{ID: value.ID, ResourceID: value.ResourceID, FieldID: value.FieldID, AuthorID: value.AuthorID, Author: value.Author, Role: string(value.Role), Body: value.Body, At: value.At}
	}
	doc.Activity = make([]activityDocument, len(ws.Activity))
	for i, value := range ws.Activity {
		doc.Activity[i] = activityDocument{ID: value.ID, Actor: value.Actor, Verb: value.Verb, Target: value.Target, ResourceID: value.ResourceID, At: value.At}
	}
	return doc
}

func toEntity(doc workspaceDocument) *entity.Workspace {
	ws := &entity.Workspace{ID: doc.ID, Rev: doc.Rev}
	for _, value := range doc.Collaborators {
		ws.Collaborators = append(ws.Collaborators, entity.Collaborator{ID: value.ID, Name: value.Name, Role: entity.CollaboratorRole(value.Role), Color: value.Color})
	}
	for _, value := range doc.Resources {
		item := entity.Resource{ID: value.ID, Name: value.Name, Kind: entity.ResourceKind(value.Kind), Method: value.Method, Path: value.Path, State: entity.FieldState(value.State), UpdatedAt: value.UpdatedAt, UpdatedBy: value.UpdatedBy}
		for _, field := range value.Fields {
			item.Fields = append(item.Fields, entity.SchemaField{ID: field.ID, Key: field.Key, Type: field.Type, Required: field.Required, State: entity.FieldState(field.State), Change: entity.FieldChange(field.Change), Description: field.Description, Value: normalizeBSONValue(field.Value)})
		}
		ws.Resources = append(ws.Resources, item)
	}
	for _, value := range doc.Comments {
		ws.Comments = append(ws.Comments, entity.Comment{ID: value.ID, ResourceID: value.ResourceID, FieldID: value.FieldID, AuthorID: value.AuthorID, Author: value.Author, Role: entity.CollaboratorRole(value.Role), Body: value.Body, At: value.At})
	}
	for _, value := range doc.Activity {
		ws.Activity = append(ws.Activity, entity.ActivityEvent{ID: value.ID, Actor: value.Actor, Verb: value.Verb, Target: value.Target, ResourceID: value.ResourceID, At: value.At})
	}
	return ws
}

func normalizeBSONValue(value any) any {
	switch value := value.(type) {
	case bson.D:
		out := make(map[string]any, len(value))
		for _, item := range value {
			out[item.Key] = normalizeBSONValue(item.Value)
		}
		return out
	case bson.M:
		out := make(map[string]any, len(value))
		for key, item := range value {
			out[key] = normalizeBSONValue(item)
		}
		return out
	case bson.A:
		out := make([]any, len(value))
		for i, item := range value {
			out[i] = normalizeBSONValue(item)
		}
		return out
	default:
		return value
	}
}
