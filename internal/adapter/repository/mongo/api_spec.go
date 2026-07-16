package mongo

import (
	"context"
	"errors"
	"fmt"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"kingdom_manager/backend/internal/domain/entity"
	"kingdom_manager/backend/internal/domain/port"
	"time"
)

type APISpecRepository struct {
	revisions, counters *mongo.Collection
	client              *mongo.Client
}

func NewAPISpecRepository(database *mongo.Database, client *mongo.Client) *APISpecRepository {
	return &APISpecRepository{revisions: database.Collection("api_spec_revisions"), counters: database.Collection("api_spec_counters"), client: client}
}
func (r *APISpecRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.revisions.Indexes().CreateMany(ctx, []mongo.IndexModel{{Keys: bson.D{{Key: "project_id", Value: 1}, {Key: "content_hash", Value: 1}}, Options: options.Index().SetUnique(true)}, {Keys: bson.D{{Key: "project_id", Value: 1}, {Key: "number", Value: -1}}, Options: options.Index().SetUnique(true)}, {Keys: bson.D{{Key: "project_id", Value: 1}, {Key: "status", Value: 1}}}})
	return err
}

type apiSpecDoc struct {
	ID                 string    `bson:"_id"`
	ProjectID          string    `bson:"project_id"`
	PreviousRevisionID string    `bson:"previous_revision_id"`
	Number             int64     `bson:"number"`
	Status             string    `bson:"status"`
	SourceFilename     string    `bson:"source_filename"`
	Format             string    `bson:"format"`
	Content            string    `bson:"content"`
	ContentHash        string    `bson:"content_hash"`
	Message            string    `bson:"message"`
	GitBranch          string    `bson:"git_branch"`
	GitCommitSHA       string    `bson:"git_commit_sha"`
	CreatedByTokenID   string    `bson:"created_by_token_id"`
	CreatedAt          time.Time `bson:"created_at"`
}

func toAPIDoc(v *entity.APISpecRevision) apiSpecDoc {
	return apiSpecDoc{v.ID, v.ProjectID, v.PreviousRevisionID, v.Number, v.Status, v.SourceFilename, v.Format, v.Content, v.ContentHash, v.Message, v.GitBranch, v.GitCommitSHA, v.CreatedByTokenID, v.CreatedAt}
}
func toAPIEntity(v *apiSpecDoc) *entity.APISpecRevision {
	return &entity.APISpecRevision{ID: v.ID, ProjectID: v.ProjectID, PreviousRevisionID: v.PreviousRevisionID, Number: v.Number, Status: v.Status, SourceFilename: v.SourceFilename, Format: v.Format, Content: v.Content, ContentHash: v.ContentHash, Message: v.Message, GitBranch: v.GitBranch, GitCommitSHA: v.GitCommitSHA, CreatedByTokenID: v.CreatedByTokenID, CreatedAt: v.CreatedAt}
}
func (r *APISpecRepository) Publish(ctx context.Context, value *entity.APISpecRevision) (*entity.APISpecRevision, bool, error) {
	session, err := r.client.StartSession()
	if err != nil {
		return nil, false, err
	}
	defer session.EndSession(ctx)
	var result *entity.APISpecRevision
	_, err = session.WithTransaction(ctx, func(sc mongo.SessionContext) (any, error) {
		var existing apiSpecDoc
		err := r.revisions.FindOne(sc, bson.M{"project_id": value.ProjectID, "content_hash": value.ContentHash}).Decode(&existing)
		if err == nil {
			result = toAPIEntity(&existing)
			return nil, nil
		}
		if !errors.Is(err, mongo.ErrNoDocuments) {
			return nil, err
		}
		var previous apiSpecDoc
		_ = r.revisions.FindOne(sc, bson.M{"project_id": value.ProjectID, "status": "current"}).Decode(&previous)
		update := r.counters.FindOneAndUpdate(sc, bson.M{"_id": value.ProjectID}, bson.M{"$inc": bson.M{"number": 1}}, options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After))
		var counter struct {
			Number int64 `bson:"number"`
		}
		if err := update.Decode(&counter); err != nil {
			return nil, err
		}
		value.Number = counter.Number
		value.PreviousRevisionID = previous.ID
		if _, err := r.revisions.UpdateMany(sc, bson.M{"project_id": value.ProjectID, "status": "current"}, bson.M{"$set": bson.M{"status": "superseded"}}); err != nil {
			return nil, err
		}
		if _, err := r.revisions.InsertOne(sc, toAPIDoc(value)); err != nil {
			return nil, err
		}
		result = value
		return nil, nil
	})
	if err != nil {
		return nil, false, fmt.Errorf("publish transaction: %w", err)
	}
	return result, result.ID != "" && result.ID != value.ID, nil
}
func (r *APISpecRepository) Current(ctx context.Context, projectID string) (*entity.APISpecRevision, error) {
	var d apiSpecDoc
	if err := r.revisions.FindOne(ctx, bson.M{"project_id": projectID, "status": "current"}).Decode(&d); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, port.ErrAPISpecNotFound
		}
		return nil, err
	}
	return toAPIEntity(&d), nil
}
func (r *APISpecRepository) Get(ctx context.Context, projectID, id string) (*entity.APISpecRevision, error) {
	var d apiSpecDoc
	if err := r.revisions.FindOne(ctx, bson.M{"_id": id, "project_id": projectID}).Decode(&d); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, port.ErrAPISpecNotFound
		}
		return nil, err
	}
	return toAPIEntity(&d), nil
}
func (r *APISpecRepository) List(ctx context.Context, projectID string) ([]entity.APISpecRevision, error) {
	c, err := r.revisions.Find(ctx, bson.M{"project_id": projectID}, options.Find().SetSort(bson.D{{Key: "number", Value: -1}}))
	if err != nil {
		return nil, err
	}
	defer c.Close(ctx)
	var d []apiSpecDoc
	if err := c.All(ctx, &d); err != nil {
		return nil, err
	}
	out := make([]entity.APISpecRevision, 0, len(d))
	for i := range d {
		out = append(out, *toAPIEntity(&d[i]))
	}
	return out, nil
}
