package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"kingdom_manager/backend/internal/adapter/http/middleware"
	"kingdom_manager/backend/internal/domain/entity"
	"kingdom_manager/backend/internal/domain/port"
	"kingdom_manager/backend/internal/usecase"
)

const deleteAllResourcesConfirmationHeader = "X-Confirm-Delete-All"

type Handler struct {
	service         *usecase.Service
	roomService     *usecase.RoomService
	flowService     *usecase.FlowService
	storyService    *usecase.StoryService
	proposalService *usecase.ProposalService
	feedbackService *usecase.FeedbackService
	executor        port.HTTPExecutor
	auth            *middleware.Auth
}

func New(service *usecase.Service, roomService *usecase.RoomService, flowService *usecase.FlowService, storyService *usecase.StoryService, proposalService *usecase.ProposalService, feedbackService *usecase.FeedbackService, executor port.HTTPExecutor, auth *middleware.Auth) *Handler {
	return &Handler{service: service, roomService: roomService, flowService: flowService, storyService: storyService, proposalService: proposalService, feedbackService: feedbackService, executor: executor, auth: auth}
}

func (h *Handler) serviceFor(c *gin.Context) *usecase.Service {
	return h.service.ForWorkspace(middleware.WorkspaceID(c))
}

func success(c *gin.Context, status int, data any) {
	c.JSON(status, gin.H{"success": true, "message": "OK", "data": data})
}

func (h *Handler) Workspace(c *gin.Context) {
	ws, err := h.serviceFor(c).Snapshot(c.Request.Context())
	if err != nil {
		h.writeError(c, err)
		return
	}
	success(c, http.StatusOK, workspaceDTO(ws))
}

func (h *Handler) Collaborators(c *gin.Context) {
	ws, err := h.serviceFor(c).Snapshot(c.Request.Context())
	if err != nil {
		h.writeError(c, err)
		return
	}
	items := make([]collaboratorResponse, 0, len(ws.Collaborators))
	for _, value := range ws.Collaborators {
		items = append(items, collaboratorDTO(value))
	}
	success(c, http.StatusOK, items)
}

func (h *Handler) Me(c *gin.Context) {
	value, err := h.serviceFor(c).Me(c.Request.Context(), middleware.CollaboratorID(c))
	if err != nil {
		h.writeError(c, err)
		return
	}
	success(c, http.StatusOK, collaboratorDTO(*value))
}

func (h *Handler) ListResources(c *gin.Context) {
	items, err := h.serviceFor(c).Resources(c.Request.Context(), c.Query("kind"), c.Query("status"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	out := make([]resourceResponse, 0, len(items))
	for _, item := range items {
		out = append(out, resourceDTO(item))
	}
	success(c, http.StatusOK, out)
}

func (h *Handler) GetResource(c *gin.Context) {
	value, err := h.serviceFor(c).Resource(c.Request.Context(), c.Param("id"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	success(c, http.StatusOK, resourceDTO(*value))
}

type createResourceRequest struct {
	Name   string `json:"name" binding:"required"`
	Kind   string `json:"kind" binding:"required"`
	Method string `json:"method"`
	Path   string `json:"path"`
}

func (h *Handler) CreateResource(c *gin.Context) {
	var request createResourceRequest
	if !bind(c, &request) {
		return
	}
	result, err := h.serviceFor(c).CreateResource(c.Request.Context(), middleware.CollaboratorID(c), revision(c), usecase.CreateResourceInput{Name: request.Name, Kind: request.Kind, Method: request.Method, Path: request.Path})
	h.writeMutation(c, result, err, http.StatusCreated)
}

type importResourcesRequest struct {
	Endpoints []importEndpointRequest `json:"endpoints" binding:"required"`
}

type importEndpointRequest struct {
	Name      string                  `json:"name" binding:"required"`
	Method    string                  `json:"method"`
	Path      string                  `json:"path"`
	Fields    []importFieldRequest    `json:"fields"`
	Responses []responseSchemaRequest `json:"responses"`
}

type importFieldRequest struct {
	Key         string  `json:"key"`
	Type        string  `json:"type"`
	Required    bool    `json:"required"`
	Description *string `json:"description"`
	Value       any     `json:"value"`
}

// ImportResources bulk-creates endpoints from a parsed spec in one mutation.
func (h *Handler) ImportResources(c *gin.Context) {
	var request importResourcesRequest
	if !bind(c, &request) {
		return
	}
	inputs := make([]usecase.ImportEndpointInput, len(request.Endpoints))
	for i, ep := range request.Endpoints {
		fields := make([]usecase.ImportFieldInput, len(ep.Fields))
		for j, f := range ep.Fields {
			fields[j] = usecase.ImportFieldInput{Key: f.Key, Type: f.Type, Required: f.Required, Description: f.Description, Value: f.Value}
		}
		responses := make([]usecase.ResponseSchemaInput, len(ep.Responses))
		for j, response := range ep.Responses {
			respFields := make([]usecase.ResponseFieldInput, len(response.Fields))
			for k, field := range response.Fields {
				respFields[k] = toSchemaFieldInput(field)
			}
			responses[j] = usecase.ResponseSchemaInput{Status: response.Status, Description: response.Description, Fields: respFields}
		}
		inputs[i] = usecase.ImportEndpointInput{Name: ep.Name, Method: ep.Method, Path: ep.Path, Fields: fields, Responses: responses}
	}
	result, err := h.serviceFor(c).ImportResources(c.Request.Context(), middleware.CollaboratorID(c), revision(c), inputs)
	if err != nil {
		h.writeError(c, err)
		return
	}
	resources := make([]resourceResponse, len(result.Resources))
	for i, resource := range result.Resources {
		resources[i] = resourceDTO(resource)
	}
	success(c, http.StatusCreated, gin.H{"rev": result.Rev, "resources": resources})
}

type updateResourceRequest struct {
	Name   *string `json:"name"`
	Method *string `json:"method"`
	Path   *string `json:"path"`
	Status *string `json:"status"`
}

func (h *Handler) UpdateResource(c *gin.Context) {
	var request updateResourceRequest
	if !bind(c, &request) {
		return
	}
	result, err := h.serviceFor(c).UpdateResource(c.Request.Context(), middleware.CollaboratorID(c), c.Param("id"), revision(c), usecase.UpdateResourceInput{Name: request.Name, Method: request.Method, Path: request.Path, Status: request.Status})
	h.writeMutation(c, result, err, http.StatusOK)
}

func (h *Handler) DeleteAllResources(c *gin.Context) {
	if c.GetHeader(deleteAllResourcesConfirmationHeader) != "true" {
		c.JSON(http.StatusUnprocessableEntity, gin.H{
			"success": false,
			"message": "bulk resource deletion requires explicit confirmation",
			"data":    nil,
			"error": gin.H{
				"code":    "VALIDATION_ERROR",
				"details": gin.H{"header": deleteAllResourcesConfirmationHeader},
			},
		})
		return
	}
	result, err := h.serviceFor(c).DeleteAllResources(c.Request.Context(), middleware.CollaboratorID(c), revision(c))
	if err != nil {
		h.writeError(c, err)
		return
	}
	success(c, http.StatusOK, gin.H{"rev": result.Rev, "resource_ids": result.ResourceIDs})
}

func (h *Handler) DeleteResource(c *gin.Context) {
	result, err := h.serviceFor(c).DeleteResource(c.Request.Context(), middleware.CollaboratorID(c), c.Param("id"), revision(c))
	if err != nil {
		h.writeError(c, err)
		return
	}
	success(c, http.StatusOK, gin.H{"rev": result.Rev, "resource_id": c.Param("id")})
}

type replaceResponsesRequest struct {
	Responses *[]responseSchemaRequest `json:"responses" binding:"required"`
}

type responseSchemaRequest struct {
	Status      int                  `json:"status"`
	Description *string              `json:"description"`
	Fields      []schemaFieldRequest `json:"fields"`
}

// schemaFieldRequest is a request/response body field as submitted by a
// client — recursive (Children/Items), shared by the response-schema
// endpoints (import + replace) and the request-field-tree endpoint below.
type schemaFieldRequest struct {
	ID          string                  `json:"id"`
	Key         string                  `json:"key"`
	Type        string                  `json:"type"`
	Required    bool                    `json:"required"`
	Nullable    bool                    `json:"nullable"`
	State       string                  `json:"state"`
	Change      string                  `json:"change"`
	Description *string                 `json:"description"`
	Value       any                     `json:"value"`
	Example     any                     `json:"example"`
	Default     any                     `json:"default"`
	EnumValues  []string                `json:"enum_values"`
	Validation  *fieldValidationRequest `json:"validation"`
	Children    []schemaFieldRequest    `json:"children"`
	Items       *schemaFieldRequest     `json:"items"`
}

type fieldValidationRequest struct {
	MinLength *int     `json:"min_length"`
	MaxLength *int     `json:"max_length"`
	Minimum   *float64 `json:"minimum"`
	Maximum   *float64 `json:"maximum"`
	Pattern   *string  `json:"pattern"`
	Format    *string  `json:"format"`
}

func toSchemaFieldInput(r schemaFieldRequest) usecase.SchemaFieldInput {
	input := usecase.SchemaFieldInput{
		ID: r.ID, Key: r.Key, Type: r.Type, Required: r.Required, Nullable: r.Nullable,
		State: r.State, Change: r.Change, Description: r.Description,
		Value: r.Value, Example: r.Example, Default: r.Default, EnumValues: r.EnumValues,
	}
	if r.Validation != nil {
		input.Validation = &usecase.FieldValidationInput{
			MinLength: r.Validation.MinLength, MaxLength: r.Validation.MaxLength,
			Minimum: r.Validation.Minimum, Maximum: r.Validation.Maximum,
			Pattern: r.Validation.Pattern, Format: r.Validation.Format,
		}
	}
	if len(r.Children) > 0 {
		input.Children = make([]usecase.SchemaFieldInput, len(r.Children))
		for i, child := range r.Children {
			input.Children[i] = toSchemaFieldInput(child)
		}
	}
	if r.Items != nil {
		items := toSchemaFieldInput(*r.Items)
		input.Items = &items
	}
	return input
}

func (h *Handler) ReplaceResponses(c *gin.Context) {
	var request replaceResponsesRequest
	if !bind(c, &request) {
		return
	}
	inputs := make([]usecase.ResponseSchemaInput, len(*request.Responses))
	for i, response := range *request.Responses {
		fields := make([]usecase.ResponseFieldInput, len(response.Fields))
		for j, field := range response.Fields {
			fields[j] = toSchemaFieldInput(field)
		}
		inputs[i] = usecase.ResponseSchemaInput{Status: response.Status, Description: response.Description, Fields: fields}
	}
	result, err := h.serviceFor(c).ReplaceResponses(c.Request.Context(), middleware.CollaboratorID(c), c.Param("id"), revision(c), inputs)
	h.writeMutation(c, result, err, http.StatusOK)
}

type replaceRequestFieldsRequest struct {
	Fields []schemaFieldRequest `json:"fields" binding:"required"`
}

// ReplaceRequestFields bulk-replaces a resource's whole request-body field
// tree (nested objects/arrays included) — backs the Visual Builder's save.
func (h *Handler) ReplaceRequestFields(c *gin.Context) {
	var request replaceRequestFieldsRequest
	if !bind(c, &request) {
		return
	}
	inputs := make([]usecase.SchemaFieldInput, len(request.Fields))
	for i, field := range request.Fields {
		inputs[i] = toSchemaFieldInput(field)
	}
	result, err := h.serviceFor(c).ReplaceRequestFields(c.Request.Context(), middleware.CollaboratorID(c), c.Param("id"), revision(c), inputs)
	h.writeMutation(c, result, err, http.StatusOK)
}

type addFieldRequest struct {
	Key         string  `json:"key" binding:"required"`
	Type        string  `json:"type" binding:"required"`
	Required    bool    `json:"required"`
	State       string  `json:"state"`
	Description *string `json:"description"`
}

func (h *Handler) AddField(c *gin.Context) {
	var request addFieldRequest
	if !bind(c, &request) {
		return
	}
	result, err := h.serviceFor(c).AddField(c.Request.Context(), middleware.CollaboratorID(c), c.Param("id"), revision(c), usecase.FieldInput{Key: request.Key, Type: request.Type, Required: request.Required, State: request.State, Description: request.Description})
	h.writeMutation(c, result, err, http.StatusCreated)
}

type updateFieldRequest struct {
	Key         *string         `json:"key"`
	Type        *string         `json:"type"`
	Required    *bool           `json:"required"`
	State       *string         `json:"state"`
	Description *optionalString `json:"description"`
	Value       optionalJSON    `json:"value"`
}
type optionalString struct{ Value *string }
type optionalJSON struct {
	Set   bool
	Value any
}

func (o *optionalString) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		o.Value = nil
		return nil
	}
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	o.Value = &value
	return nil
}

func (o *optionalJSON) UnmarshalJSON(data []byte) error {
	o.Set = true
	return json.Unmarshal(data, &o.Value)
}

func (h *Handler) UpdateField(c *gin.Context) {
	var request updateFieldRequest
	if !bind(c, &request) {
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
	result, err := h.serviceFor(c).UpdateField(c.Request.Context(), middleware.CollaboratorID(c), c.Param("id"), c.Param("field_id"), revision(c), usecase.UpdateFieldInput{Key: request.Key, Type: request.Type, Required: request.Required, State: request.State, Description: description, Value: value})
	h.writeMutation(c, result, err, http.StatusOK)
}

func (h *Handler) DeleteField(c *gin.Context) {
	result, err := h.serviceFor(c).DeleteField(c.Request.Context(), middleware.CollaboratorID(c), c.Param("id"), c.Param("field_id"), revision(c))
	h.writeMutation(c, result, err, http.StatusOK)
}

func (h *Handler) Comments(c *gin.Context) {
	items, err := h.serviceFor(c).Comments(c.Request.Context(), c.Param("id"), c.Query("field_id"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	out := make([]commentResponse, 0, len(items))
	for _, item := range items {
		out = append(out, commentDTO(item))
	}
	success(c, http.StatusOK, out)
}

type addCommentRequest struct {
	FieldID *string `json:"field_id"`
	Body    string  `json:"body" binding:"required"`
}

func (h *Handler) AddComment(c *gin.Context) {
	var request addCommentRequest
	if !bind(c, &request) {
		return
	}
	result, err := h.serviceFor(c).AddComment(c.Request.Context(), middleware.CollaboratorID(c), c.Param("id"), revision(c), request.FieldID, request.Body)
	if err != nil {
		h.writeError(c, err)
		return
	}
	success(c, http.StatusCreated, gin.H{"rev": result.Rev, "comment": commentDTO(*result.Comment)})
}

func (h *Handler) DeleteComment(c *gin.Context) {
	result, err := h.serviceFor(c).DeleteComment(c.Request.Context(), middleware.CollaboratorID(c), c.Param("id"), revision(c))
	if err != nil {
		h.writeError(c, err)
		return
	}
	success(c, http.StatusOK, gin.H{"rev": result.Rev, "comment_id": c.Param("id")})
}

func (h *Handler) ChatMessages(c *gin.Context) {
	items, err := h.serviceFor(c).ChatMessages(c.Request.Context())
	if err != nil {
		h.writeError(c, err)
		return
	}
	out := make([]chatMessageResponse, 0, len(items))
	for _, item := range items {
		out = append(out, chatMessageDTO(item))
	}
	success(c, http.StatusOK, out)
}

type sendChatMessageRequest struct {
	Body string `json:"body" binding:"required"`
}

func (h *Handler) SendChatMessage(c *gin.Context) {
	var request sendChatMessageRequest
	if !bind(c, &request) {
		return
	}
	message, err := h.serviceFor(c).SendChatMessage(c.Request.Context(), middleware.CollaboratorID(c), request.Body)
	if err != nil {
		h.writeError(c, err)
		return
	}
	success(c, http.StatusCreated, gin.H{"message": chatMessageDTO(*message)})
}

func (h *Handler) TaskLogs(c *gin.Context) {
	items, err := h.serviceFor(c).TaskLogs(c.Request.Context())
	if err != nil {
		h.writeError(c, err)
		return
	}
	out := make([]taskLogResponse, 0, len(items))
	for _, item := range items {
		out = append(out, taskLogDTO(item))
	}
	success(c, http.StatusOK, out)
}

type addTaskLogRequest struct {
	Kind       string `json:"kind"`
	Body       string `json:"body" binding:"required"`
	ResourceID string `json:"resource_id"`
}

func (h *Handler) AddTaskLog(c *gin.Context) {
	var request addTaskLogRequest
	if !bind(c, &request) {
		return
	}
	entry, err := h.serviceFor(c).AddTaskLog(c.Request.Context(), middleware.CollaboratorID(c), entity.TaskLogKind(request.Kind), request.Body, request.ResourceID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	success(c, http.StatusCreated, gin.H{"task_log": taskLogDTO(*entry)})
}

func (h *Handler) Activity(c *gin.Context) {
	page, limit := positiveInt(c.Query("page"), 1), positiveInt(c.Query("limit"), 50)
	if limit > 100 {
		limit = 100
	}
	items, total, err := h.serviceFor(c).Activity(c.Request.Context(), c.Query("resource_id"), page, limit)
	if err != nil {
		h.writeError(c, err)
		return
	}
	out := make([]activityResponse, 0, len(items))
	for _, item := range items {
		out = append(out, activityDTO(item))
	}
	success(c, http.StatusOK, gin.H{"items": out, "page_info": gin.H{"page": page, "limit": limit, "total": total}})
}

func (h *Handler) writeMutation(c *gin.Context, result *usecase.MutationResult, err error, status int) {
	if err != nil {
		h.writeError(c, err)
		return
	}
	success(c, status, gin.H{"rev": result.Rev, "resource": resourceDTO(*result.Resource)})
}

func bind(c *gin.Context, target any) bool {
	if err := c.ShouldBindJSON(target); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid request body", "data": nil, "error": gin.H{"code": "VALIDATION_ERROR", "details": err.Error()}})
		return false
	}
	return true
}

func revision(c *gin.Context) *int64 {
	value := c.GetHeader("If-Match")
	if value == "" {
		return nil
	}
	rev, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return nil
	}
	return &rev
}
func positiveInt(value string, fallback int) int {
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 1 {
		return fallback
	}
	return parsed
}

func (h *Handler) writeError(c *gin.Context, err error) {
	status, code, message := http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error"
	var details map[string]any
	var data any
	var appErr *usecase.Error
	if errors.As(err, &appErr) {
		message, details = appErr.Message, appErr.Details
	}
	switch {
	case errors.Is(err, usecase.ErrValidation):
		status, code = http.StatusUnprocessableEntity, "VALIDATION_ERROR"
	case errors.Is(err, usecase.ErrNotFound):
		status, code = http.StatusNotFound, "NOT_FOUND"
	case errors.Is(err, usecase.ErrForbidden):
		status, code = http.StatusForbidden, "FORBIDDEN"
	case errors.Is(err, usecase.ErrRevConflict):
		status, code = http.StatusConflict, "REV_CONFLICT"
		if h.service != nil && middleware.WorkspaceID(c) != "" {
			if workspace, snapshotErr := h.serviceFor(c).Snapshot(c.Request.Context()); snapshotErr == nil {
				data = workspaceDTO(workspace)
			}
		}
	}
	c.JSON(status, gin.H{"success": false, "message": message, "data": data, "error": gin.H{"code": code, "details": details}})
}
