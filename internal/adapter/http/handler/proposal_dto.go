package handler

import (
	"time"

	"kingdom_manager/backend/internal/domain/entity"
	"kingdom_manager/backend/internal/usecase"
)

type createProposalRequest struct {
	ResourceID  string               `json:"resource_id" binding:"required"`
	Title       string               `json:"title"`
	Description string               `json:"description"`
	Fields      []schemaFieldRequest `json:"fields"`
}

type updateProposalRequest struct {
	Title       *string `json:"title"`
	Description *string `json:"description"`
}

type setProposalStatusRequest struct {
	Status string `json:"status" binding:"required"`
}

type addProposalFieldRequest struct {
	Key         string  `json:"key" binding:"required"`
	Type        string  `json:"type" binding:"required"`
	Required    bool    `json:"required"`
	Description *string `json:"description"`
}

type updateProposalFieldRequest struct {
	Key         *string         `json:"key"`
	Type        *string         `json:"type"`
	Required    *bool           `json:"required"`
	State       *string         `json:"state"`
	Description *optionalString `json:"description"`
	Value       optionalJSON    `json:"value"`
}

type addProposalCommentRequest struct {
	FieldKey string `json:"field_key"`
	Body     string `json:"body" binding:"required"`
}

type proposalCommentResponse struct {
	ID       string    `json:"id"`
	FieldKey string    `json:"field_key,omitempty"`
	Author   string    `json:"author"`
	Role     string    `json:"role"`
	Body     string    `json:"body"`
	Resolved bool      `json:"resolved"`
	At       time.Time `json:"at"`
}

type proposalTimelineResponse struct {
	ID     string    `json:"id"`
	Kind   string    `json:"kind"`
	Actor  string    `json:"actor"`
	Detail string    `json:"detail"`
	At     time.Time `json:"at"`
}

type proposalResponse struct {
	ID          string                     `json:"id"`
	WorkspaceID string                     `json:"workspace_id"`
	ResourceID  string                     `json:"resource_id"`
	Title       string                     `json:"title"`
	Description string                     `json:"description"`
	Author      string                     `json:"author"`
	AuthorRole  string                     `json:"author_role"`
	Status      string                     `json:"status"`
	Fields      []fieldResponse            `json:"fields"`
	Comments    []proposalCommentResponse  `json:"comments"`
	Timeline    []proposalTimelineResponse `json:"timeline"`
	CreatedAt   time.Time                  `json:"created_at"`
	UpdatedAt   time.Time                  `json:"updated_at"`
	UpdatedBy   string                     `json:"updated_by"`
}

func proposalDTO(p *entity.Proposal) proposalResponse {
	out := proposalResponse{
		ID: p.ID, WorkspaceID: p.WorkspaceID, ResourceID: p.ResourceID, Title: p.Title, Description: p.Description,
		Author: p.Author, AuthorRole: string(p.AuthorRole), Status: string(p.Status),
		CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt, UpdatedBy: p.UpdatedBy,
		Fields: []fieldResponse{}, Comments: []proposalCommentResponse{}, Timeline: []proposalTimelineResponse{},
	}
	for _, field := range p.Fields {
		out.Fields = append(out.Fields, fieldDTO(field))
	}
	for _, c := range p.Comments {
		out.Comments = append(out.Comments, proposalCommentResponse{
			ID: c.ID, FieldKey: c.FieldKey, Author: c.Author, Role: string(c.Role), Body: c.Body, Resolved: c.Resolved, At: c.At,
		})
	}
	for _, t := range p.Timeline {
		out.Timeline = append(out.Timeline, proposalTimelineResponse{ID: t.ID, Kind: t.Kind, Actor: t.Actor, Detail: t.Detail, At: t.At})
	}
	return out
}

func proposalFieldInputs(fields []schemaFieldRequest) []usecase.SchemaFieldInput {
	out := make([]usecase.SchemaFieldInput, len(fields))
	for i, field := range fields {
		out[i] = toSchemaFieldInput(field)
	}
	return out
}
