package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"kingdom_manager/backend/internal/adapter/http/middleware"
	"kingdom_manager/backend/internal/domain/entity"
	"kingdom_manager/backend/internal/usecase"
)

func (h *Handler) CreateProposal(c *gin.Context) {
	var request createProposalRequest
	if !bind(c, &request) {
		return
	}
	actor, err := h.currentCollaborator(c)
	if err != nil {
		h.writeError(c, err)
		return
	}
	proposal, err := h.proposalService.Create(c.Request.Context(), middleware.WorkspaceID(c), entity.Collaborator{ID: actor.ID, Name: actor.Name, Role: entity.CollaboratorRole(actor.Role), Color: actor.Color}, usecase.ProposalCreateInput{
		ResourceID: request.ResourceID, Title: request.Title, Description: request.Description, Fields: proposalFieldInputs(request.Fields),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	success(c, http.StatusCreated, proposalDTO(proposal))
}

func (h *Handler) ListProposals(c *gin.Context) {
	proposals, err := h.proposalService.List(c.Request.Context(), middleware.WorkspaceID(c))
	if err != nil {
		h.writeError(c, err)
		return
	}
	out := make([]proposalResponse, 0, len(proposals))
	for i := range proposals {
		out = append(out, proposalDTO(&proposals[i]))
	}
	success(c, http.StatusOK, out)
}

func (h *Handler) GetProposal(c *gin.Context) {
	proposal, err := h.proposalService.Get(c.Request.Context(), middleware.WorkspaceID(c), c.Param("id"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	success(c, http.StatusOK, proposalDTO(proposal))
}

func (h *Handler) UpdateProposal(c *gin.Context) {
	var request updateProposalRequest
	if !bind(c, &request) {
		return
	}
	actor, err := h.currentCollaborator(c)
	if err != nil {
		h.writeError(c, err)
		return
	}
	proposal, err := h.proposalService.Update(c.Request.Context(), middleware.WorkspaceID(c), c.Param("id"), actor.Name, usecase.ProposalUpdateInput{
		Title: request.Title, Description: request.Description,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	success(c, http.StatusOK, proposalDTO(proposal))
}

func (h *Handler) DeleteProposal(c *gin.Context) {
	if err := h.proposalService.Delete(c.Request.Context(), middleware.WorkspaceID(c), c.Param("id")); err != nil {
		h.writeError(c, err)
		return
	}
	success(c, http.StatusOK, gin.H{"proposal_id": c.Param("id")})
}

func (h *Handler) SetProposalStatus(c *gin.Context) {
	var request setProposalStatusRequest
	if !bind(c, &request) {
		return
	}
	actor, err := h.currentCollaborator(c)
	if err != nil {
		h.writeError(c, err)
		return
	}
	proposal, err := h.proposalService.SetStatus(c.Request.Context(), middleware.WorkspaceID(c), c.Param("id"), actor.Name, entity.ProposalStatus(request.Status))
	if err != nil {
		h.writeError(c, err)
		return
	}
	success(c, http.StatusOK, proposalDTO(proposal))
}

func (h *Handler) AddProposalField(c *gin.Context) {
	var request addProposalFieldRequest
	if !bind(c, &request) {
		return
	}
	actor, err := h.currentCollaborator(c)
	if err != nil {
		h.writeError(c, err)
		return
	}
	proposal, err := h.proposalService.AddField(c.Request.Context(), middleware.WorkspaceID(c), c.Param("id"), actor.Name, usecase.FieldInput{
		Key: request.Key, Type: request.Type, Required: request.Required, Description: request.Description,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	success(c, http.StatusCreated, proposalDTO(proposal))
}

func (h *Handler) UpdateProposalField(c *gin.Context) {
	var request updateProposalFieldRequest
	if !bind(c, &request) {
		return
	}
	actor, err := h.currentCollaborator(c)
	if err != nil {
		h.writeError(c, err)
		return
	}
	var description **string
	if request.Description != nil {
		description = &request.Description.Value
	}
	var value *any
	if request.Value.Set {
		value = &request.Value.Value
	}
	proposal, err := h.proposalService.UpdateField(c.Request.Context(), middleware.WorkspaceID(c), c.Param("id"), c.Param("field_id"), actor.Name, usecase.UpdateFieldInput{
		Key: request.Key, Type: request.Type, Required: request.Required, State: request.State, Description: description, Value: value,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	success(c, http.StatusOK, proposalDTO(proposal))
}

func (h *Handler) RemoveProposalField(c *gin.Context) {
	actor, err := h.currentCollaborator(c)
	if err != nil {
		h.writeError(c, err)
		return
	}
	proposal, err := h.proposalService.RemoveField(c.Request.Context(), middleware.WorkspaceID(c), c.Param("id"), c.Param("field_id"), actor.Name)
	if err != nil {
		h.writeError(c, err)
		return
	}
	success(c, http.StatusOK, proposalDTO(proposal))
}

func (h *Handler) AddProposalComment(c *gin.Context) {
	var request addProposalCommentRequest
	if !bind(c, &request) {
		return
	}
	actor, err := h.currentCollaborator(c)
	if err != nil {
		h.writeError(c, err)
		return
	}
	proposal, err := h.proposalService.AddComment(c.Request.Context(), middleware.WorkspaceID(c), c.Param("id"), entity.Collaborator{ID: actor.ID, Name: actor.Name, Role: entity.CollaboratorRole(actor.Role), Color: actor.Color}, request.FieldKey, request.Body)
	if err != nil {
		h.writeError(c, err)
		return
	}
	success(c, http.StatusCreated, proposalDTO(proposal))
}

func (h *Handler) ResolveProposalComment(c *gin.Context) {
	actor, err := h.currentCollaborator(c)
	if err != nil {
		h.writeError(c, err)
		return
	}
	proposal, err := h.proposalService.ResolveComment(c.Request.Context(), middleware.WorkspaceID(c), c.Param("id"), c.Param("comment_id"), actor.Name)
	if err != nil {
		h.writeError(c, err)
		return
	}
	success(c, http.StatusOK, proposalDTO(proposal))
}
