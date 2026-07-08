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

type ProposalRepository struct {
	proposals *mongo.Collection
}

func NewProposalRepository(database *mongo.Database) *ProposalRepository {
	return &ProposalRepository{proposals: database.Collection("proposals")}
}

func (r *ProposalRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.proposals.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "workspace_id", Value: 1}, {Key: "created_at", Value: -1}}})
	return err
}

type proposalCommentDoc struct {
	ID       string    `bson:"id"`
	FieldKey string    `bson:"field_key,omitempty"`
	Author   string    `bson:"author"`
	Role     string    `bson:"role"`
	Body     string    `bson:"body"`
	Resolved bool      `bson:"resolved,omitempty"`
	At       time.Time `bson:"at"`
}

type proposalTimelineDoc struct {
	ID     string    `bson:"id"`
	Kind   string    `bson:"kind"`
	Actor  string    `bson:"actor"`
	Detail string    `bson:"detail"`
	At     time.Time `bson:"at"`
}

type proposalDoc struct {
	ID          string                `bson:"_id"`
	WorkspaceID string                `bson:"workspace_id"`
	ResourceID  string                `bson:"resource_id"`
	Title       string                `bson:"title"`
	Description string                `bson:"description"`
	Author      string                `bson:"author"`
	AuthorRole  string                `bson:"author_role"`
	Status      string                `bson:"status"`
	Fields      []fieldDocument       `bson:"fields"`
	Comments    []proposalCommentDoc  `bson:"comments"`
	Timeline    []proposalTimelineDoc `bson:"timeline"`
	CreatedAt   time.Time             `bson:"created_at"`
	UpdatedAt   time.Time             `bson:"updated_at"`
	UpdatedBy   string                `bson:"updated_by"`
}

func (r *ProposalRepository) CreateProposal(ctx context.Context, proposal *entity.Proposal) error {
	if _, err := r.proposals.InsertOne(ctx, toProposalDoc(proposal)); err != nil {
		return fmt.Errorf("insert proposal: %w", err)
	}
	return nil
}

func (r *ProposalRepository) ListProposals(ctx context.Context, workspaceID string) ([]entity.Proposal, error) {
	cursor, err := r.proposals.Find(ctx, bson.M{"workspace_id": workspaceID}, options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}}))
	if err != nil {
		return nil, fmt.Errorf("list proposals: %w", err)
	}
	defer cursor.Close(ctx)
	var docs []proposalDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, fmt.Errorf("decode proposals: %w", err)
	}
	out := make([]entity.Proposal, 0, len(docs))
	for i := range docs {
		out = append(out, *toProposalEntity(&docs[i]))
	}
	return out, nil
}

func (r *ProposalRepository) GetProposal(ctx context.Context, id string) (*entity.Proposal, error) {
	var doc proposalDoc
	if err := r.proposals.FindOne(ctx, bson.M{"_id": id}).Decode(&doc); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, port.ErrProposalNotFound
		}
		return nil, fmt.Errorf("find proposal: %w", err)
	}
	return toProposalEntity(&doc), nil
}

func (r *ProposalRepository) UpdateProposal(ctx context.Context, proposal *entity.Proposal) error {
	result, err := r.proposals.ReplaceOne(ctx, bson.M{"_id": proposal.ID, "workspace_id": proposal.WorkspaceID}, toProposalDoc(proposal))
	if err != nil {
		return fmt.Errorf("replace proposal: %w", err)
	}
	if result.MatchedCount == 0 {
		return port.ErrProposalNotFound
	}
	return nil
}

func (r *ProposalRepository) DeleteProposal(ctx context.Context, workspaceID, id string) (bool, error) {
	result, err := r.proposals.DeleteOne(ctx, bson.M{"_id": id, "workspace_id": workspaceID})
	if err != nil {
		return false, fmt.Errorf("delete proposal: %w", err)
	}
	return result.DeletedCount > 0, nil
}

func toProposalDoc(p *entity.Proposal) proposalDoc {
	doc := proposalDoc{
		ID: p.ID, WorkspaceID: p.WorkspaceID, ResourceID: p.ResourceID, Title: p.Title, Description: p.Description,
		Author: p.Author, AuthorRole: string(p.AuthorRole), Status: string(p.Status),
		CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt, UpdatedBy: p.UpdatedBy,
		Fields: make([]fieldDocument, len(p.Fields)), Comments: make([]proposalCommentDoc, len(p.Comments)),
		Timeline: make([]proposalTimelineDoc, len(p.Timeline)),
	}
	for i, field := range p.Fields {
		doc.Fields[i] = toFieldDocument(field)
	}
	for i, c := range p.Comments {
		doc.Comments[i] = proposalCommentDoc{ID: c.ID, FieldKey: c.FieldKey, Author: c.Author, Role: string(c.Role), Body: c.Body, Resolved: c.Resolved, At: c.At}
	}
	for i, t := range p.Timeline {
		doc.Timeline[i] = proposalTimelineDoc{ID: t.ID, Kind: t.Kind, Actor: t.Actor, Detail: t.Detail, At: t.At}
	}
	return doc
}

func toProposalEntity(doc *proposalDoc) *entity.Proposal {
	p := &entity.Proposal{
		ID: doc.ID, WorkspaceID: doc.WorkspaceID, ResourceID: doc.ResourceID, Title: doc.Title, Description: doc.Description,
		Author: doc.Author, AuthorRole: entity.CollaboratorRole(doc.AuthorRole), Status: entity.ProposalStatus(doc.Status),
		CreatedAt: doc.CreatedAt, UpdatedAt: doc.UpdatedAt, UpdatedBy: doc.UpdatedBy,
		Fields:   make([]entity.SchemaField, 0, len(doc.Fields)),
		Comments: make([]entity.ProposalComment, 0, len(doc.Comments)),
		Timeline: make([]entity.ProposalTimelineEntry, 0, len(doc.Timeline)),
	}
	for _, field := range doc.Fields {
		p.Fields = append(p.Fields, toFieldEntity(field))
	}
	for _, c := range doc.Comments {
		p.Comments = append(p.Comments, entity.ProposalComment{ID: c.ID, FieldKey: c.FieldKey, Author: c.Author, Role: entity.CollaboratorRole(c.Role), Body: c.Body, Resolved: c.Resolved, At: c.At})
	}
	for _, t := range doc.Timeline {
		p.Timeline = append(p.Timeline, entity.ProposalTimelineEntry{ID: t.ID, Kind: t.Kind, Actor: t.Actor, Detail: t.Detail, At: t.At})
	}
	return p
}
