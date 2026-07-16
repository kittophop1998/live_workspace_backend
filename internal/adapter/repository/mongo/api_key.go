package mongo

import (
	"context"
	"errors"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"kingdom_manager/backend/internal/domain/entity"
	"kingdom_manager/backend/internal/domain/port"
	"time"
)

type APIKeyRepository struct{ keys *mongo.Collection }

func NewAPIKeyRepository(database *mongo.Database) *APIKeyRepository {
	return &APIKeyRepository{database.Collection("api_keys")}
}
func (r *APIKeyRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.keys.Indexes().CreateMany(ctx, []mongo.IndexModel{{Keys: bson.D{{Key: "secret_hash", Value: 1}}, Options: options.Index().SetUnique(true)}, {Keys: bson.D{{Key: "project_id", Value: 1}, {Key: "created_at", Value: -1}}}})
	return err
}

type apiKeyDoc struct {
	ID         string     `bson:"_id"`
	ProjectID  string     `bson:"project_id"`
	Prefix     string     `bson:"prefix"`
	SecretHash string     `bson:"secret_hash"`
	Name       string     `bson:"name"`
	Scopes     []string   `bson:"scopes"`
	CreatedBy  string     `bson:"created_by"`
	CreatedAt  time.Time  `bson:"created_at"`
	ExpiresAt  *time.Time `bson:"expires_at,omitempty"`
	RevokedAt  *time.Time `bson:"revoked_at,omitempty"`
	LastUsedAt *time.Time `bson:"last_used_at,omitempty"`
}

func toKeyDoc(v *entity.APIKey) apiKeyDoc {
	return apiKeyDoc{v.ID, v.ProjectID, v.Prefix, v.SecretHash, v.Name, v.Scopes, v.CreatedBy, v.CreatedAt, v.ExpiresAt, v.RevokedAt, v.LastUsedAt}
}
func toKeyEntity(v *apiKeyDoc) *entity.APIKey {
	return &entity.APIKey{ID: v.ID, ProjectID: v.ProjectID, Prefix: v.Prefix, SecretHash: v.SecretHash, Name: v.Name, Scopes: v.Scopes, CreatedBy: v.CreatedBy, CreatedAt: v.CreatedAt, ExpiresAt: v.ExpiresAt, RevokedAt: v.RevokedAt, LastUsedAt: v.LastUsedAt}
}
func (r *APIKeyRepository) Create(ctx context.Context, v *entity.APIKey) error {
	_, err := r.keys.InsertOne(ctx, toKeyDoc(v))
	return err
}
func (r *APIKeyRepository) FindByHash(ctx context.Context, hash string) (*entity.APIKey, error) {
	var d apiKeyDoc
	if err := r.keys.FindOne(ctx, bson.M{"secret_hash": hash}).Decode(&d); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, port.ErrAPISpecNotFound
		}
		return nil, err
	}
	return toKeyEntity(&d), nil
}
func (r *APIKeyRepository) List(ctx context.Context, projectID string) ([]entity.APIKey, error) {
	c, err := r.keys.Find(ctx, bson.M{"project_id": projectID}, options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}}))
	if err != nil {
		return nil, err
	}
	defer c.Close(ctx)
	var d []apiKeyDoc
	if err := c.All(ctx, &d); err != nil {
		return nil, err
	}
	out := make([]entity.APIKey, 0, len(d))
	for i := range d {
		out = append(out, *toKeyEntity(&d[i]))
	}
	return out, nil
}
func (r *APIKeyRepository) Revoke(ctx context.Context, projectID, id string) error {
	now := time.Now().UTC()
	result, err := r.keys.UpdateOne(ctx, bson.M{"_id": id, "project_id": projectID, "revoked_at": bson.M{"$exists": false}}, bson.M{"$set": bson.M{"revoked_at": now}})
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return port.ErrAPISpecNotFound
	}
	return nil
}
func (r *APIKeyRepository) Touch(ctx context.Context, id string) error {
	now := time.Now().UTC()
	_, err := r.keys.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": bson.M{"last_used_at": now}})
	return err
}
