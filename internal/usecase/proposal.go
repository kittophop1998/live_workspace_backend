package usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"kingdom_manager/backend/internal/domain/entity"
	"kingdom_manager/backend/internal/domain/port"
)

type ProposalService struct {
	repo port.ProposalRepository
	now  func() time.Time
}

func NewProposalService(repo port.ProposalRepository) *ProposalService {
	return &ProposalService{repo: repo, now: func() time.Time { return time.Now().UTC() }}
}

type ProposalCreateInput struct {
	ResourceID  string
	Title       string
	Description string
	Fields      []SchemaFieldInput
}

type ProposalUpdateInput struct {
	Title       *string
	Description *string
}

var proposalStatusVerbs = map[entity.ProposalStatus]string{
	entity.ProposalStatusDraft:     "moved to draft",
	entity.ProposalStatusReviewing: "requested review",
	entity.ProposalStatusApproved:  "approved this proposal",
	entity.ProposalStatusRejected:  "rejected this proposal",
	entity.ProposalStatusMerged:    "merged this proposal",
}

func (s *ProposalService) Create(ctx context.Context, workspaceID string, actor entity.Collaborator, in ProposalCreateInput) (*entity.Proposal, error) {
	resourceID := strings.TrimSpace(in.ResourceID)
	if resourceID == "" {
		return nil, validation("proposal resource_id is required", nil)
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		title = "Untitled proposal"
	}
	fields, err := buildFieldTree(in.Fields, fieldTreeOptions{generateMissingFieldIDs: true, defaultState: entity.StateReady, defaultChange: entity.ChangeStable})
	if err != nil {
		return nil, err
	}
	now := s.now()
	proposal := &entity.Proposal{
		ID: "prp_" + shortID(), WorkspaceID: workspaceID, ResourceID: resourceID,
		Title: title, Description: strings.TrimSpace(in.Description),
		Author: actor.Name, AuthorRole: actor.Role, Status: entity.ProposalStatusDraft,
		Fields:    fields,
		Comments:  []entity.ProposalComment{},
		Timeline:  []entity.ProposalTimelineEntry{proposalTimelineEntry("created", actor.Name, "created this proposal", now)},
		CreatedAt: now, UpdatedAt: now, UpdatedBy: actor.Name,
	}
	if err := s.repo.CreateProposal(ctx, proposal); err != nil {
		return nil, fmt.Errorf("create proposal: %w", err)
	}
	return proposal, nil
}

func (s *ProposalService) List(ctx context.Context, workspaceID string) ([]entity.Proposal, error) {
	return s.repo.ListProposals(ctx, workspaceID)
}

func (s *ProposalService) Get(ctx context.Context, workspaceID, id string) (*entity.Proposal, error) {
	proposal, err := s.repo.GetProposal(ctx, id)
	if err != nil {
		if errors.Is(err, port.ErrProposalNotFound) {
			return nil, notFound("proposal", id)
		}
		return nil, err
	}
	if proposal.WorkspaceID != workspaceID {
		return nil, notFound("proposal", id)
	}
	return proposal, nil
}

func (s *ProposalService) Update(ctx context.Context, workspaceID, id, actorName string, in ProposalUpdateInput) (*entity.Proposal, error) {
	proposal, err := s.Get(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	if in.Title != nil {
		title := strings.TrimSpace(*in.Title)
		if title == "" {
			return nil, validation("proposal title cannot be empty", nil)
		}
		proposal.Title = title
	}
	if in.Description != nil {
		proposal.Description = strings.TrimSpace(*in.Description)
	}
	return s.persist(ctx, proposal, actorName)
}

func (s *ProposalService) Delete(ctx context.Context, workspaceID, id string) error {
	deleted, err := s.repo.DeleteProposal(ctx, workspaceID, id)
	if err != nil {
		return fmt.Errorf("delete proposal: %w", err)
	}
	if !deleted {
		return notFound("proposal", id)
	}
	return nil
}

func (s *ProposalService) SetStatus(ctx context.Context, workspaceID, id, actorName string, status entity.ProposalStatus) (*entity.Proposal, error) {
	verb, ok := proposalStatusVerbs[status]
	if !ok {
		return nil, validation("invalid proposal status", map[string]any{"status": status})
	}
	proposal, err := s.Get(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	proposal.Status = status
	proposal.Timeline = append(proposal.Timeline, proposalTimelineEntry("status", actorName, verb, s.now()))
	return s.persist(ctx, proposal, actorName)
}

func (s *ProposalService) AddField(ctx context.Context, workspaceID, id, actorName string, in FieldInput) (*entity.Proposal, error) {
	proposal, err := s.Get(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	key := strings.TrimSpace(in.Key)
	if key == "" || !fieldTypes[in.Type] {
		return nil, validation("invalid field", map[string]any{"key": in.Key, "type": in.Type})
	}
	for _, f := range proposal.Fields {
		if f.Key == key {
			return nil, validation("duplicate field key", map[string]any{"key": key})
		}
	}
	proposal.Fields = append(proposal.Fields, entity.SchemaField{
		ID: "fld_" + shortID(), Key: key, Type: in.Type, Required: in.Required,
		State: entity.StateDraft, Change: entity.ChangeAdded, Description: in.Description,
	})
	proposal.Timeline = append(proposal.Timeline, proposalTimelineEntry("field", actorName, fmt.Sprintf("added %q", key), s.now()))
	return s.persist(ctx, proposal, actorName)
}

func (s *ProposalService) UpdateField(ctx context.Context, workspaceID, id, fieldID, actorName string, in UpdateFieldInput) (*entity.Proposal, error) {
	proposal, err := s.Get(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	index := proposalFieldIndex(proposal, fieldID)
	if index == -1 {
		return nil, notFound("proposal field", fieldID)
	}
	field := proposal.Fields[index]
	label := field.Key
	if in.Key != nil {
		key := strings.TrimSpace(*in.Key)
		if key == "" {
			return nil, validation("field key cannot be empty", nil)
		}
		field.Key = key
		label = key
	}
	if in.Type != nil {
		if !fieldTypes[*in.Type] {
			return nil, validation("invalid field type", map[string]any{"type": *in.Type})
		}
		field.Type = *in.Type
	}
	if in.Required != nil {
		field.Required = *in.Required
	}
	if in.State != nil {
		if !validState(*in.State) {
			return nil, validation("invalid field state", map[string]any{"state": *in.State})
		}
		field.State = entity.FieldState(*in.State)
	}
	if in.Description != nil {
		field.Description = *in.Description
	}
	if in.Value != nil {
		field.Value = *in.Value
	}
	proposal.Fields[index] = field
	proposal.Timeline = append(proposal.Timeline, proposalTimelineEntry("field", actorName, fmt.Sprintf("edited %q", label), s.now()))
	return s.persist(ctx, proposal, actorName)
}

func (s *ProposalService) RemoveField(ctx context.Context, workspaceID, id, fieldID, actorName string) (*entity.Proposal, error) {
	proposal, err := s.Get(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	index := proposalFieldIndex(proposal, fieldID)
	if index == -1 {
		return nil, notFound("proposal field", fieldID)
	}
	label := proposal.Fields[index].Key
	proposal.Fields = append(proposal.Fields[:index], proposal.Fields[index+1:]...)
	proposal.Timeline = append(proposal.Timeline, proposalTimelineEntry("field", actorName, fmt.Sprintf("removed %q", label), s.now()))
	return s.persist(ctx, proposal, actorName)
}

func (s *ProposalService) AddComment(ctx context.Context, workspaceID, id string, actor entity.Collaborator, fieldKey, body string) (*entity.Proposal, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return nil, validation("comment body is required", nil)
	}
	proposal, err := s.Get(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	now := s.now()
	proposal.Comments = append(proposal.Comments, entity.ProposalComment{
		ID: "prc_" + shortID(), FieldKey: fieldKey, Author: actor.Name, Role: actor.Role, Body: body, At: now,
	})
	detail := "commented"
	if fieldKey != "" {
		detail = fmt.Sprintf("commented on %q", fieldKey)
	}
	proposal.Timeline = append(proposal.Timeline, proposalTimelineEntry("comment", actor.Name, detail, now))
	return s.persist(ctx, proposal, actor.Name)
}

func (s *ProposalService) ResolveComment(ctx context.Context, workspaceID, id, commentID, actorName string) (*entity.Proposal, error) {
	proposal, err := s.Get(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	index := -1
	for i, c := range proposal.Comments {
		if c.ID == commentID {
			index = i
			break
		}
	}
	if index == -1 {
		return nil, notFound("proposal comment", commentID)
	}
	proposal.Comments[index].Resolved = !proposal.Comments[index].Resolved
	return s.persist(ctx, proposal, actorName)
}

func (s *ProposalService) persist(ctx context.Context, proposal *entity.Proposal, actorName string) (*entity.Proposal, error) {
	proposal.UpdatedAt = s.now()
	proposal.UpdatedBy = actorName
	if err := s.repo.UpdateProposal(ctx, proposal); err != nil {
		if errors.Is(err, port.ErrProposalNotFound) {
			return nil, notFound("proposal", proposal.ID)
		}
		return nil, fmt.Errorf("update proposal: %w", err)
	}
	return proposal, nil
}

func proposalFieldIndex(proposal *entity.Proposal, fieldID string) int {
	for i, f := range proposal.Fields {
		if f.ID == fieldID {
			return i
		}
	}
	return -1
}

func proposalTimelineEntry(kind, actor, detail string, at time.Time) entity.ProposalTimelineEntry {
	return entity.ProposalTimelineEntry{ID: "ptl_" + shortID(), Kind: kind, Actor: actor, Detail: detail, At: at}
}
