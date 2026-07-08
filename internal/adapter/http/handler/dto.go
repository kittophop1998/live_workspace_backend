package handler

import (
	"time"

	"github.com/gin-gonic/gin"

	"kingdom_manager/backend/internal/domain/entity"
)

func workspaceDTO(ws *entity.Workspace) gin.H {
	resources := make([]resourceResponse, 0, len(ws.Resources))
	for _, value := range ws.Resources {
		resources = append(resources, resourceDTO(value))
	}
	comments := make([]commentResponse, 0, len(ws.Comments))
	for _, value := range ws.Comments {
		comments = append(comments, commentDTO(value))
	}
	activity := make([]activityResponse, 0, len(ws.Activity))
	for i := len(ws.Activity) - 1; i >= 0; i-- {
		activity = append(activity, activityDTO(ws.Activity[i]))
	}
	collaborators := make([]collaboratorResponse, 0, len(ws.Collaborators))
	for _, value := range ws.Collaborators {
		collaborators = append(collaborators, collaboratorDTO(value))
	}
	return gin.H{
		"rev": ws.Rev, "workspace_id": ws.ID, "resources": resources,
		"comments": comments, "activity": activity, "collaborators": collaborators,
		"server_time": time.Now().UTC(),
	}
}

type collaboratorResponse struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Role  string `json:"role"`
	Color string `json:"color"`
}
type fieldValidationResponse struct {
	MinLength *int     `json:"min_length,omitempty"`
	MaxLength *int     `json:"max_length,omitempty"`
	Minimum   *float64 `json:"minimum,omitempty"`
	Maximum   *float64 `json:"maximum,omitempty"`
	Pattern   *string  `json:"pattern,omitempty"`
	Format    *string  `json:"format,omitempty"`
}
type fieldResponse struct {
	ID          string                   `json:"id"`
	Key         string                   `json:"key"`
	Type        string                   `json:"type"`
	Required    bool                     `json:"required"`
	Nullable    bool                     `json:"nullable,omitempty"`
	State       string                   `json:"state"`
	Change      string                   `json:"change"`
	Description *string                  `json:"description,omitempty"`
	Value       any                      `json:"value,omitempty"`
	Example     any                      `json:"example,omitempty"`
	Default     any                      `json:"default,omitempty"`
	EnumValues  []string                 `json:"enum_values,omitempty"`
	Validation  *fieldValidationResponse `json:"validation,omitempty"`
	Children    []fieldResponse          `json:"children,omitempty"`
	Items       *fieldResponse           `json:"items,omitempty"`
}
type responseSchemaResponse struct {
	Status      int             `json:"status"`
	Description *string         `json:"description,omitempty"`
	Fields      []fieldResponse `json:"fields"`
}
type resourceResponse struct {
	ID        string                    `json:"id"`
	Name      string                    `json:"name"`
	Kind      string                    `json:"kind"`
	Method    *string                   `json:"method"`
	Path      *string                   `json:"path"`
	State     string                    `json:"state"`
	Status    *string                   `json:"status"`
	Fields    []fieldResponse           `json:"fields"`
	Responses *[]responseSchemaResponse `json:"responses,omitempty"`
	UpdatedAt time.Time                 `json:"updated_at"`
	UpdatedBy string                    `json:"updated_by"`
}
type commentResponse struct {
	ID         string    `json:"id"`
	ResourceID string    `json:"resource_id"`
	FieldID    *string   `json:"field_id"`
	Author     string    `json:"author"`
	Role       string    `json:"role"`
	Body       string    `json:"body"`
	At         time.Time `json:"at"`
}
type activityResponse struct {
	ID         string    `json:"id"`
	Actor      string    `json:"actor"`
	Verb       string    `json:"verb"`
	Target     string    `json:"target"`
	ResourceID string    `json:"resource_id"`
	At         time.Time `json:"at"`
}

func collaboratorDTO(value entity.Collaborator) collaboratorResponse {
	return collaboratorResponse{ID: value.ID, Name: value.Name, Role: string(value.Role), Color: value.Color}
}
func resourceDTO(value entity.Resource) resourceResponse {
	var status *string
	if value.Status != nil {
		text := string(*value.Status)
		status = &text
	}
	out := resourceResponse{ID: value.ID, Name: value.Name, Kind: string(value.Kind), Method: value.Method, Path: value.Path, State: string(value.State), Status: status, Fields: []fieldResponse{}, UpdatedAt: value.UpdatedAt, UpdatedBy: value.UpdatedBy}
	for _, field := range value.Fields {
		out.Fields = append(out.Fields, fieldDTO(field))
	}
	if value.Kind == entity.KindEndpoint {
		responses := make([]responseSchemaResponse, len(value.Responses))
		for i, response := range value.Responses {
			fields := make([]fieldResponse, len(response.Fields))
			for j, field := range response.Fields {
				fields[j] = fieldDTO(field)
			}
			responses[i] = responseSchemaResponse{Status: response.Status, Description: response.Description, Fields: fields}
		}
		out.Responses = &responses
	}
	return out
}
func fieldDTO(field entity.SchemaField) fieldResponse {
	out := fieldResponse{
		ID: field.ID, Key: field.Key, Type: field.Type, Required: field.Required, Nullable: field.Nullable,
		State: string(field.State), Change: string(field.Change), Description: field.Description,
		Value: field.Value, Example: field.Example, Default: field.Default, EnumValues: field.EnumValues,
	}
	if field.Validation != nil {
		out.Validation = &fieldValidationResponse{
			MinLength: field.Validation.MinLength, MaxLength: field.Validation.MaxLength,
			Minimum: field.Validation.Minimum, Maximum: field.Validation.Maximum,
			Pattern: field.Validation.Pattern, Format: field.Validation.Format,
		}
	}
	if len(field.Children) > 0 {
		out.Children = make([]fieldResponse, len(field.Children))
		for i, child := range field.Children {
			out.Children[i] = fieldDTO(child)
		}
	}
	if field.Items != nil {
		items := fieldDTO(*field.Items)
		out.Items = &items
	}
	return out
}
func commentDTO(value entity.Comment) commentResponse {
	return commentResponse{ID: value.ID, ResourceID: value.ResourceID, FieldID: value.FieldID, Author: value.Author, Role: string(value.Role), Body: value.Body, At: value.At}
}
func activityDTO(value entity.ActivityEvent) activityResponse {
	return activityResponse{ID: value.ID, Actor: value.Actor, Verb: value.Verb, Target: value.Target, ResourceID: value.ResourceID, At: value.At}
}
