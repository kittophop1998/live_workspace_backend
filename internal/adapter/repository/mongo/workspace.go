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

const (
	workspacesCollection    = "workspaces"
	resourcesCollection     = "resources"
	commentsCollection      = "comments"
	activitiesCollection    = "activities"
	collaboratorsCollection = "collaborators"
)

// WorkspaceRepository stores the workspace aggregate in separate collections.
// Child documents are versioned so a new aggregate can be staged before the
// workspace revision is atomically published.
type WorkspaceRepository struct {
	workspaces    *mongo.Collection
	resources     *mongo.Collection
	comments      *mongo.Collection
	activities    *mongo.Collection
	collaborators *mongo.Collection
}

func NewWorkspaceRepository(database *mongo.Database) *WorkspaceRepository {
	return &WorkspaceRepository{
		workspaces:    database.Collection(workspacesCollection),
		resources:     database.Collection(resourcesCollection),
		comments:      database.Collection(commentsCollection),
		activities:    database.Collection(activitiesCollection),
		collaborators: database.Collection(collaboratorsCollection),
	}
}

type workspaceDocument struct {
	ID  string `bson:"_id"`
	Rev int64  `bson:"rev"`

	// Legacy embedded fields are read only by MigrateLegacy.
	Resources     []legacyResourceDocument     `bson:"resources,omitempty"`
	Comments      []legacyCommentDocument      `bson:"comments,omitempty"`
	Activity      []legacyActivityDocument     `bson:"activity,omitempty"`
	Collaborators []legacyCollaboratorDocument `bson:"collaborators,omitempty"`
}

type RevisionDocument struct {
	DocumentID   string `bson:"_id"`
	WorkspaceID  string `bson:"workspace_id"`
	WorkspaceRev int64  `bson:"workspace_rev"`
	Position     int    `bson:"position"`
}

type collaboratorDocument struct {
	RevisionDocument `bson:",inline"`
	ID               string `bson:"id"`
	Name             string `bson:"name"`
	Role             string `bson:"role"`
	Color            string `bson:"color"`
}

type fieldDocument struct {
	ID          string  `bson:"id"`
	Key         string  `bson:"key"`
	Type        string  `bson:"type"`
	State       string  `bson:"state"`
	Change      string  `bson:"change"`
	Required    bool    `bson:"required"`
	Description *string `bson:"description,omitempty"`
	Value       any     `bson:"value,omitempty"`
}

type responseSchemaDocument struct {
	Status      int             `bson:"status"`
	Description *string         `bson:"description,omitempty"`
	Fields      []fieldDocument `bson:"fields"`
}

type resourceDocument struct {
	RevisionDocument `bson:",inline"`
	ID               string                   `bson:"id"`
	Name             string                   `bson:"name"`
	Kind             string                   `bson:"kind"`
	State            string                   `bson:"state"`
	UpdatedBy        string                   `bson:"updated_by"`
	Method           *string                  `bson:"method,omitempty"`
	Path             *string                  `bson:"path,omitempty"`
	Status           *string                  `bson:"status,omitempty"`
	Fields           []fieldDocument          `bson:"fields"`
	Responses        []responseSchemaDocument `bson:"responses,omitempty"`
	UpdatedAt        time.Time                `bson:"updated_at"`
}

type commentDocument struct {
	RevisionDocument `bson:",inline"`
	ID               string    `bson:"id"`
	ResourceID       string    `bson:"resource_id"`
	AuthorID         string    `bson:"author_id"`
	Author           string    `bson:"author"`
	Role             string    `bson:"role"`
	Body             string    `bson:"body"`
	FieldID          *string   `bson:"field_id,omitempty"`
	At               time.Time `bson:"at"`
}

type activityDocument struct {
	RevisionDocument `bson:",inline"`
	ID               string    `bson:"id"`
	Actor            string    `bson:"actor"`
	Verb             string    `bson:"verb"`
	Target           string    `bson:"target"`
	ResourceID       string    `bson:"resource_id,omitempty"`
	At               time.Time `bson:"at"`
}

// These types intentionally have no BSON tags. The old repository relied on
// the driver's default lower-cased Go field names (for example "resourceid"
// and "updatedat"), so migration must decode that exact shape.
type legacyCollaboratorDocument struct {
	ID, Name, Role, Color string
}

type legacyFieldDocument struct {
	ID, Key, Type, State, Change string
	Required                     bool
	Description                  *string
	Value                        any
}

type legacyResponseSchemaDocument struct {
	Status      int
	Description *string
	Fields      []legacyFieldDocument
}

type legacyResourceDocument struct {
	ID, Name, Kind, State, UpdatedBy string
	Method, Path                     *string
	Status                           *string
	Fields                           []legacyFieldDocument
	Responses                        []legacyResponseSchemaDocument
	UpdatedAt                        time.Time
}

type legacyCommentDocument struct {
	ID, ResourceID, AuthorID, Author, Role, Body string
	FieldID                                      *string
	At                                           time.Time
}

type legacyActivityDocument struct {
	ID, Actor, Verb, Target, ResourceID string
	At                                  time.Time
}

func (r *WorkspaceRepository) Get(ctx context.Context, id string) (*entity.Workspace, error) {
	for attempt := 0; attempt < 3; attempt++ {
		var metadata workspaceDocument
		if err := r.workspaces.FindOne(ctx, bson.M{"_id": id}).Decode(&metadata); err != nil {
			if errors.Is(err, mongo.ErrNoDocuments) {
				return nil, port.ErrWorkspaceNotFound
			}
			return nil, fmt.Errorf("find workspace: %w", err)
		}

		workspace, err := r.loadRevision(ctx, metadata.ID, metadata.Rev)
		if err != nil {
			return nil, err
		}
		var current workspaceDocument
		err = r.workspaces.FindOne(ctx, bson.M{"_id": id}, options.FindOne().SetProjection(bson.M{"rev": 1})).Decode(&current)
		if err != nil {
			return nil, fmt.Errorf("verify workspace revision: %w", err)
		}
		if current.Rev == metadata.Rev {
			return workspace, nil
		}
	}
	return nil, port.ErrRevisionConflict
}

func (r *WorkspaceRepository) CreateIfAbsent(ctx context.Context, workspace *entity.Workspace) error {
	result, err := r.workspaces.UpdateOne(
		ctx,
		bson.M{"_id": workspace.ID},
		bson.M{"$setOnInsert": workspaceDocument{ID: workspace.ID, Rev: workspace.Rev}},
		options.Update().SetUpsert(true),
	)
	if err != nil {
		return fmt.Errorf("seed workspace: %w", err)
	}
	if result.UpsertedCount == 0 {
		return nil
	}
	if err := r.insertRevision(ctx, workspace); err != nil {
		_, _ = r.workspaces.DeleteOne(ctx, bson.M{"_id": workspace.ID, "rev": workspace.Rev})
		return fmt.Errorf("seed workspace children: %w", err)
	}
	return nil
}

func (r *WorkspaceRepository) Create(ctx context.Context, workspace *entity.Workspace) error {
	if _, err := r.workspaces.InsertOne(ctx, workspaceDocument{ID: workspace.ID, Rev: workspace.Rev}); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return port.ErrWorkspaceExists
		}
		return fmt.Errorf("insert workspace: %w", err)
	}
	if err := r.insertRevision(ctx, workspace); err != nil {
		_, _ = r.workspaces.DeleteOne(ctx, bson.M{"_id": workspace.ID, "rev": workspace.Rev})
		_ = r.deleteRevision(ctx, workspace.ID, workspace.Rev)
		return fmt.Errorf("insert workspace children: %w", err)
	}
	return nil
}

func (r *WorkspaceRepository) Save(ctx context.Context, workspace *entity.Workspace, expectedRev int64) error {
	if workspace.Rev <= expectedRev {
		return fmt.Errorf("save workspace: revision %d must be greater than %d", workspace.Rev, expectedRev)
	}
	if err := r.insertRevision(ctx, workspace); err != nil {
		_ = r.deleteRevision(ctx, workspace.ID, workspace.Rev)
		return fmt.Errorf("stage workspace revision: %w", err)
	}

	result, err := r.workspaces.UpdateOne(
		ctx,
		bson.M{"_id": workspace.ID, "rev": expectedRev},
		bson.M{"$set": bson.M{"rev": workspace.Rev}},
	)
	if err != nil {
		_ = r.deleteRevision(ctx, workspace.ID, workspace.Rev)
		return fmt.Errorf("publish workspace revision: %w", err)
	}
	if result.MatchedCount == 0 {
		_ = r.deleteRevision(ctx, workspace.ID, workspace.Rev)
		return port.ErrRevisionConflict
	}

	// Old versions are no longer reachable. Cleanup is best-effort because it
	// must not turn an already-published save into an apparent failure.
	_ = r.deleteOtherRevisions(ctx, workspace.ID, workspace.Rev)
	return nil
}

func (r *WorkspaceRepository) EnsureIndexes(ctx context.Context) error {
	indexes := []struct {
		collection *mongo.Collection
		models     []mongo.IndexModel
	}{
		{r.resources, []mongo.IndexModel{
			{Keys: bson.D{{Key: "workspace_id", Value: 1}, {Key: "workspace_rev", Value: 1}, {Key: "position", Value: 1}}},
			{Keys: bson.D{{Key: "workspace_id", Value: 1}, {Key: "kind", Value: 1}}},
		}},
		{r.comments, []mongo.IndexModel{
			{Keys: bson.D{{Key: "workspace_id", Value: 1}, {Key: "workspace_rev", Value: 1}, {Key: "position", Value: 1}}},
			{Keys: bson.D{{Key: "workspace_id", Value: 1}, {Key: "resource_id", Value: 1}}},
		}},
		{r.activities, []mongo.IndexModel{
			{Keys: bson.D{{Key: "workspace_id", Value: 1}, {Key: "workspace_rev", Value: 1}, {Key: "at", Value: -1}}},
		}},
		{r.collaborators, []mongo.IndexModel{
			{Keys: bson.D{{Key: "workspace_id", Value: 1}, {Key: "workspace_rev", Value: 1}, {Key: "position", Value: 1}}},
		}},
	}
	for _, item := range indexes {
		if _, err := item.collection.Indexes().CreateMany(ctx, item.models); err != nil {
			return fmt.Errorf("create %s indexes: %w", item.collection.Name(), err)
		}
	}
	return nil
}

// MigrateLegacy moves arrays embedded in old workspace documents into the
// normalized collections. It is idempotent and safe to run on every startup.
func (r *WorkspaceRepository) MigrateLegacy(ctx context.Context) error {
	filter := bson.M{"$or": bson.A{
		bson.M{"resources": bson.M{"$exists": true}},
		bson.M{"comments": bson.M{"$exists": true}},
		bson.M{"activity": bson.M{"$exists": true}},
		bson.M{"collaborators": bson.M{"$exists": true}},
	}}
	cursor, err := r.workspaces.Find(ctx, filter)
	if err != nil {
		return fmt.Errorf("find legacy workspaces: %w", err)
	}
	defer cursor.Close(ctx)

	for cursor.Next(ctx) {
		var legacy workspaceDocument
		if err := cursor.Decode(&legacy); err != nil {
			return fmt.Errorf("decode legacy workspace: %w", err)
		}
		workspace := toLegacyEntity(legacy)
		if err := r.upsertRevision(ctx, workspace); err != nil {
			return fmt.Errorf("migrate workspace %s: %w", legacy.ID, err)
		}
		_, err := r.workspaces.UpdateOne(
			ctx,
			bson.M{"_id": legacy.ID, "rev": legacy.Rev},
			bson.M{"$unset": bson.M{"resources": "", "comments": "", "activity": "", "collaborators": ""}},
		)
		if err != nil {
			return fmt.Errorf("finalize workspace %s migration: %w", legacy.ID, err)
		}
	}
	if err := cursor.Err(); err != nil {
		return fmt.Errorf("iterate legacy workspaces: %w", err)
	}
	return nil
}

func (r *WorkspaceRepository) loadRevision(ctx context.Context, workspaceID string, rev int64) (*entity.Workspace, error) {
	filter := bson.M{"workspace_id": workspaceID, "workspace_rev": rev}
	var resources []resourceDocument
	var comments []commentDocument
	var activities []activityDocument
	var collaborators []collaboratorDocument
	if err := findAll(ctx, r.resources, filter, &resources); err != nil {
		return nil, err
	}
	if err := findAll(ctx, r.comments, filter, &comments); err != nil {
		return nil, err
	}
	if err := findAll(ctx, r.activities, filter, &activities); err != nil {
		return nil, err
	}
	if err := findAll(ctx, r.collaborators, filter, &collaborators); err != nil {
		return nil, err
	}
	return toEntity(workspaceID, rev, resources, comments, activities, collaborators), nil
}

func findAll(ctx context.Context, collection *mongo.Collection, filter any, destination any) error {
	cursor, err := collection.Find(ctx, filter, options.Find().SetSort(bson.D{{Key: "position", Value: 1}}))
	if err != nil {
		return fmt.Errorf("find %s: %w", collection.Name(), err)
	}
	defer cursor.Close(ctx)
	if err := cursor.All(ctx, destination); err != nil {
		return fmt.Errorf("decode %s: %w", collection.Name(), err)
	}
	return nil
}

func (r *WorkspaceRepository) upsertRevision(ctx context.Context, workspace *entity.Workspace) error {
	resources, comments, activities, collaborators := toDocuments(workspace)
	upserts := []struct {
		collection *mongo.Collection
		documents  []any
	}{
		{r.resources, resources},
		{r.comments, comments},
		{r.activities, activities},
		{r.collaborators, collaborators},
	}
	for _, upsert := range upserts {
		for _, document := range upsert.documents {
			raw, err := bson.Marshal(document)
			if err != nil {
				return fmt.Errorf("marshal %s migration document: %w", upsert.collection.Name(), err)
			}
			var fields bson.M
			if err := bson.Unmarshal(raw, &fields); err != nil {
				return fmt.Errorf("decode %s migration document: %w", upsert.collection.Name(), err)
			}
			if _, err := upsert.collection.ReplaceOne(
				ctx,
				bson.M{"_id": fields["_id"]},
				document,
				options.Replace().SetUpsert(true),
			); err != nil {
				return fmt.Errorf("upsert %s: %w", upsert.collection.Name(), err)
			}
		}
	}
	return nil
}

func (r *WorkspaceRepository) insertRevision(ctx context.Context, workspace *entity.Workspace) error {
	resources, comments, activities, collaborators := toDocuments(workspace)
	insertions := []struct {
		collection *mongo.Collection
		documents  []any
	}{
		{r.resources, resources},
		{r.comments, comments},
		{r.activities, activities},
		{r.collaborators, collaborators},
	}
	for _, insertion := range insertions {
		if len(insertion.documents) == 0 {
			continue
		}
		if _, err := insertion.collection.InsertMany(ctx, insertion.documents); err != nil {
			return fmt.Errorf("insert %s: %w", insertion.collection.Name(), err)
		}
	}
	return nil
}

func (r *WorkspaceRepository) deleteRevision(ctx context.Context, workspaceID string, rev int64) error {
	filter := bson.M{"workspace_id": workspaceID, "workspace_rev": rev}
	for _, collection := range r.childCollections() {
		if _, err := collection.DeleteMany(ctx, filter); err != nil {
			return fmt.Errorf("delete %s revision: %w", collection.Name(), err)
		}
	}
	return nil
}

func (r *WorkspaceRepository) deleteOtherRevisions(ctx context.Context, workspaceID string, rev int64) error {
	filter := bson.M{"workspace_id": workspaceID, "workspace_rev": bson.M{"$ne": rev}}
	for _, collection := range r.childCollections() {
		if _, err := collection.DeleteMany(ctx, filter); err != nil {
			return fmt.Errorf("delete stale %s revisions: %w", collection.Name(), err)
		}
	}
	return nil
}

func (r *WorkspaceRepository) childCollections() []*mongo.Collection {
	return []*mongo.Collection{r.resources, r.comments, r.activities, r.collaborators}
}

func toDocuments(workspace *entity.Workspace) (resources, comments, activities, collaborators []any) {
	resources = make([]any, 0, len(workspace.Resources))
	for position, value := range workspace.Resources {
		resources = append(resources, toResourceDocument(workspace.ID, workspace.Rev, position, value))
	}
	comments = make([]any, 0, len(workspace.Comments))
	for position, value := range workspace.Comments {
		comments = append(comments, commentDocument{
			RevisionDocument: child(workspace.ID, workspace.Rev, position, value.ID),
			ID:               value.ID, ResourceID: value.ResourceID, FieldID: value.FieldID,
			AuthorID: value.AuthorID, Author: value.Author, Role: string(value.Role), Body: value.Body, At: value.At,
		})
	}
	activities = make([]any, 0, len(workspace.Activity))
	for position, value := range workspace.Activity {
		activities = append(activities, activityDocument{
			RevisionDocument: child(workspace.ID, workspace.Rev, position, value.ID),
			ID:               value.ID, Actor: value.Actor, Verb: value.Verb, Target: value.Target, ResourceID: value.ResourceID, At: value.At,
		})
	}
	collaborators = make([]any, 0, len(workspace.Collaborators))
	for position, value := range workspace.Collaborators {
		collaborators = append(collaborators, collaboratorDocument{
			RevisionDocument: child(workspace.ID, workspace.Rev, position, value.ID),
			ID:               value.ID, Name: value.Name, Role: string(value.Role), Color: value.Color,
		})
	}
	return resources, comments, activities, collaborators
}

func child(workspaceID string, rev int64, position int, id string) RevisionDocument {
	return RevisionDocument{
		DocumentID:  workspaceID + ":" + fmt.Sprint(rev) + ":" + id,
		WorkspaceID: workspaceID, WorkspaceRev: rev, Position: position,
	}
}

func toResourceDocument(workspaceID string, rev int64, position int, value entity.Resource) resourceDocument {
	var status *string
	if value.Status != nil {
		text := string(*value.Status)
		status = &text
	}
	item := resourceDocument{
		RevisionDocument: child(workspaceID, rev, position, value.ID),
		ID:               value.ID, Name: value.Name, Kind: string(value.Kind), Method: value.Method, Path: value.Path,
		State: string(value.State), Status: status, UpdatedAt: value.UpdatedAt, UpdatedBy: value.UpdatedBy,
		Fields: make([]fieldDocument, len(value.Fields)),
	}
	for i, field := range value.Fields {
		item.Fields[i] = toFieldDocument(field)
	}
	if value.Kind == entity.KindEndpoint {
		item.Responses = make([]responseSchemaDocument, len(value.Responses))
		for i, response := range value.Responses {
			fields := make([]fieldDocument, len(response.Fields))
			for j, field := range response.Fields {
				fields[j] = toFieldDocument(field)
			}
			item.Responses[i] = responseSchemaDocument{Status: response.Status, Description: response.Description, Fields: fields}
		}
	}
	return item
}

func toEntity(workspaceID string, rev int64, resources []resourceDocument, comments []commentDocument, activities []activityDocument, collaborators []collaboratorDocument) *entity.Workspace {
	workspace := &entity.Workspace{
		ID: workspaceID, Rev: rev,
		Resources:     make([]entity.Resource, 0, len(resources)),
		Comments:      make([]entity.Comment, 0, len(comments)),
		Activity:      make([]entity.ActivityEvent, 0, len(activities)),
		Collaborators: make([]entity.Collaborator, 0, len(collaborators)),
	}
	for _, value := range collaborators {
		workspace.Collaborators = append(workspace.Collaborators, entity.Collaborator{ID: value.ID, Name: value.Name, Role: entity.CollaboratorRole(value.Role), Color: value.Color})
	}
	for _, value := range resources {
		var status *entity.EndpointStatus
		if value.Kind == string(entity.KindEndpoint) {
			text := entity.EndpointStatusDraft
			if value.Status != nil {
				text = entity.EndpointStatus(*value.Status)
			}
			status = &text
		}
		item := entity.Resource{
			ID: value.ID, Name: value.Name, Kind: entity.ResourceKind(value.Kind), Method: value.Method, Path: value.Path,
			State: entity.FieldState(value.State), Status: status, UpdatedAt: value.UpdatedAt, UpdatedBy: value.UpdatedBy,
			Fields: make([]entity.SchemaField, 0, len(value.Fields)),
		}
		for _, field := range value.Fields {
			item.Fields = append(item.Fields, toFieldEntity(field))
		}
		if value.Kind == string(entity.KindEndpoint) {
			item.Responses = make([]entity.ResponseSchema, len(value.Responses))
			for i, response := range value.Responses {
				fields := make([]entity.SchemaField, len(response.Fields))
				for j, field := range response.Fields {
					fields[j] = toFieldEntity(field)
				}
				item.Responses[i] = entity.ResponseSchema{Status: response.Status, Description: response.Description, Fields: fields}
			}
		}
		workspace.Resources = append(workspace.Resources, item)
	}
	for _, value := range comments {
		workspace.Comments = append(workspace.Comments, entity.Comment{ID: value.ID, ResourceID: value.ResourceID, FieldID: value.FieldID, AuthorID: value.AuthorID, Author: value.Author, Role: entity.CollaboratorRole(value.Role), Body: value.Body, At: value.At})
	}
	for _, value := range activities {
		workspace.Activity = append(workspace.Activity, entity.ActivityEvent{ID: value.ID, Actor: value.Actor, Verb: value.Verb, Target: value.Target, ResourceID: value.ResourceID, At: value.At})
	}
	return workspace
}

func toLegacyEntity(document workspaceDocument) *entity.Workspace {
	workspace := &entity.Workspace{
		ID:            document.ID,
		Rev:           document.Rev,
		Resources:     make([]entity.Resource, 0, len(document.Resources)),
		Comments:      make([]entity.Comment, 0, len(document.Comments)),
		Activity:      make([]entity.ActivityEvent, 0, len(document.Activity)),
		Collaborators: make([]entity.Collaborator, 0, len(document.Collaborators)),
	}
	for _, value := range document.Collaborators {
		workspace.Collaborators = append(workspace.Collaborators, entity.Collaborator{
			ID: value.ID, Name: value.Name, Role: entity.CollaboratorRole(value.Role), Color: value.Color,
		})
	}
	for _, value := range document.Resources {
		var status *entity.EndpointStatus
		if value.Kind == string(entity.KindEndpoint) {
			text := entity.EndpointStatusDraft
			if value.Status != nil {
				text = entity.EndpointStatus(*value.Status)
			}
			status = &text
		}
		resource := entity.Resource{
			ID: value.ID, Name: value.Name, Kind: entity.ResourceKind(value.Kind),
			Method: value.Method, Path: value.Path, State: entity.FieldState(value.State),
			Status: status, UpdatedAt: value.UpdatedAt, UpdatedBy: value.UpdatedBy,
			Fields: make([]entity.SchemaField, 0, len(value.Fields)),
		}
		for _, field := range value.Fields {
			resource.Fields = append(resource.Fields, legacyFieldEntity(field))
		}
		if value.Kind == string(entity.KindEndpoint) {
			resource.Responses = make([]entity.ResponseSchema, len(value.Responses))
			for i, response := range value.Responses {
				fields := make([]entity.SchemaField, len(response.Fields))
				for j, field := range response.Fields {
					fields[j] = legacyFieldEntity(field)
				}
				resource.Responses[i] = entity.ResponseSchema{
					Status: response.Status, Description: response.Description, Fields: fields,
				}
			}
		}
		workspace.Resources = append(workspace.Resources, resource)
	}
	for _, value := range document.Comments {
		workspace.Comments = append(workspace.Comments, entity.Comment{
			ID: value.ID, ResourceID: value.ResourceID, FieldID: value.FieldID,
			AuthorID: value.AuthorID, Author: value.Author, Role: entity.CollaboratorRole(value.Role),
			Body: value.Body, At: value.At,
		})
	}
	for _, value := range document.Activity {
		workspace.Activity = append(workspace.Activity, entity.ActivityEvent{
			ID: value.ID, Actor: value.Actor, Verb: value.Verb,
			Target: value.Target, ResourceID: value.ResourceID, At: value.At,
		})
	}
	return workspace
}

func legacyFieldEntity(field legacyFieldDocument) entity.SchemaField {
	return entity.SchemaField{
		ID: field.ID, Key: field.Key, Type: field.Type, Required: field.Required,
		State: entity.FieldState(field.State), Change: entity.FieldChange(field.Change),
		Description: field.Description, Value: normalizeBSONValue(field.Value),
	}
}

func toFieldDocument(field entity.SchemaField) fieldDocument {
	return fieldDocument{
		ID: field.ID, Key: field.Key, Type: field.Type, Required: field.Required,
		State: string(field.State), Change: string(field.Change),
		Description: field.Description, Value: field.Value,
	}
}

func toFieldEntity(field fieldDocument) entity.SchemaField {
	return entity.SchemaField{
		ID: field.ID, Key: field.Key, Type: field.Type, Required: field.Required,
		State: entity.FieldState(field.State), Change: entity.FieldChange(field.Change),
		Description: field.Description, Value: normalizeBSONValue(field.Value),
	}
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
