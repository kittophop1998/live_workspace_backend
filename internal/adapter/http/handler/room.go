package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type createRoomRequest struct {
	Name string `json:"name" binding:"required"`
}

type joinRoomRequest struct {
	RoomCode string `json:"room_code" binding:"required"`
	Name     string `json:"name" binding:"required"`
}

func (h *Handler) CreateRoom(c *gin.Context) {
	var request createRoomRequest
	if !bind(c, &request) {
		return
	}
	session, err := h.roomService.Create(c.Request.Context(), request.Name)
	if err != nil {
		h.writeError(c, err)
		return
	}
	h.writeRoomSession(c, http.StatusCreated, session.Workspace.ID, session.Collaborator.ID, collaboratorDTO(session.Collaborator), workspaceDTO(session.Workspace))
}

func (h *Handler) JoinRoom(c *gin.Context) {
	var request joinRoomRequest
	if !bind(c, &request) {
		return
	}
	session, err := h.roomService.Join(c.Request.Context(), request.RoomCode, request.Name)
	if err != nil {
		h.writeError(c, err)
		return
	}
	h.writeRoomSession(c, http.StatusOK, session.Workspace.ID, session.Collaborator.ID, collaboratorDTO(session.Collaborator), workspaceDTO(session.Workspace))
}

func (h *Handler) writeRoomSession(c *gin.Context, status int, roomCode, collaboratorID string, collaborator collaboratorResponse, session gin.H) {
	token, err := h.auth.Issue(collaboratorID, roomCode)
	if err != nil {
		h.writeError(c, err)
		return
	}
	success(c, status, gin.H{
		"access_token": token, "token_type": "Bearer", "room_code": roomCode,
		"collaborator": collaborator, "session": session,
	})
}
