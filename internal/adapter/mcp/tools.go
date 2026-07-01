package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"kingdom_manager/backend/internal/domain/entity"
	"kingdom_manager/backend/internal/domain/port"
	"kingdom_manager/backend/internal/usecase"
)

type toolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

func toolDefinitions() []toolDefinition {
	project := objectSchema(map[string]any{"project_id": stringProperty("Authenticated workspace/project ID")}, "project_id")
	endpoint := objectSchema(map[string]any{
		"project_id":  stringProperty("Authenticated workspace/project ID"),
		"endpoint_id": stringProperty("Endpoint resource ID"),
	}, "project_id", "endpoint_id")
	return []toolDefinition{
		{Name: "listProjects", Description: "List the project available to the authenticated user.", InputSchema: objectSchema(nil)},
		{Name: "getProject", Description: "Get the authenticated project workspace.", InputSchema: project},
		{Name: "listEndpoints", Description: "List endpoint resources in a project.", InputSchema: project},
		{Name: "getEndpoint", Description: "Get one endpoint resource.", InputSchema: endpoint},
		{Name: "getOpenAPISpec", Description: "Build an OpenAPI 3.1 document from project endpoint resources.", InputSchema: project},
		{Name: "getJSONSchema", Description: "Build JSON Schema for a resource.", InputSchema: objectSchema(map[string]any{
			"project_id":  stringProperty("Authenticated workspace/project ID"),
			"resource_id": stringProperty("Resource or endpoint ID"),
		}, "project_id", "resource_id")},
		{Name: "getWorkflow", Description: "Get one saved workflow.", InputSchema: objectSchema(map[string]any{
			"project_id":  stringProperty("Authenticated workspace/project ID"),
			"workflow_id": stringProperty("Workflow ID"),
		}, "project_id", "workflow_id")},
		{Name: "listComments", Description: "List comments on an endpoint or resource.", InputSchema: objectSchema(map[string]any{
			"project_id":  stringProperty("Authenticated workspace/project ID"),
			"endpoint_id": stringProperty("Endpoint or resource ID"),
			"field_id":    stringProperty("Optional field ID filter"),
		}, "project_id", "endpoint_id")},
	}
}

func objectSchema(properties map[string]any, required ...string) map[string]any {
	if properties == nil {
		properties = map[string]any{}
	}
	schema := map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func stringProperty(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

type arguments struct {
	ProjectID  string `json:"project_id"`
	EndpointID string `json:"endpoint_id"`
	ResourceID string `json:"resource_id"`
	WorkflowID string `json:"workflow_id"`
	FieldID    string `json:"field_id"`
}

func (s *Server) execute(ctx context.Context, workspaceID, userID, name string, raw json.RawMessage) (any, string, error) {
	var args arguments
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&args); err != nil {
		return nil, "", toolInputError("validation failed: invalid arguments")
	}
	service := s.workspaces.ForWorkspace(workspaceID)
	if _, err := service.Me(ctx, userID); err != nil {
		if errors.Is(err, usecase.ErrNotFound) {
			return nil, args.ProjectID, toolPermissionError("user is not a member of this workspace")
		}
		return nil, args.ProjectID, err
	}

	switch name {
	case "listProjects":
		ws, err := service.Snapshot(ctx)
		if err != nil {
			return nil, "", err
		}
		return []any{projectSummary(ws)}, "", nil
	case "getProject":
		if err := requireProject(args.ProjectID, workspaceID); err != nil {
			return nil, args.ProjectID, err
		}
		ws, err := service.Snapshot(ctx)
		return ws, args.ProjectID, err
	case "listEndpoints":
		if err := requireProject(args.ProjectID, workspaceID); err != nil {
			return nil, args.ProjectID, err
		}
		value, err := service.Resources(ctx, string(entity.KindEndpoint), "")
		return value, args.ProjectID, err
	case "getEndpoint":
		if err := requireProject(args.ProjectID, workspaceID); err != nil {
			return nil, args.ProjectID, err
		}
		value, err := service.Resource(ctx, args.EndpointID)
		if err == nil && value.Kind != entity.KindEndpoint {
			err = toolNotFoundError("endpoint not found")
		}
		return value, args.ProjectID, err
	case "getOpenAPISpec":
		if err := requireProject(args.ProjectID, workspaceID); err != nil {
			return nil, args.ProjectID, err
		}
		value, err := service.Resources(ctx, string(entity.KindEndpoint), "")
		if err != nil {
			return nil, args.ProjectID, err
		}
		return openAPISpec(args.ProjectID, value), args.ProjectID, nil
	case "getJSONSchema":
		if err := requireProject(args.ProjectID, workspaceID); err != nil {
			return nil, args.ProjectID, err
		}
		value, err := service.Resource(ctx, args.ResourceID)
		if err != nil {
			return nil, args.ProjectID, err
		}
		return jsonSchema(*value), args.ProjectID, nil
	case "getWorkflow":
		if err := requireProject(args.ProjectID, workspaceID); err != nil {
			return nil, args.ProjectID, err
		}
		value, err := s.flows.Get(ctx, workspaceID, args.WorkflowID)
		return value, args.ProjectID, err
	case "listComments":
		if err := requireProject(args.ProjectID, workspaceID); err != nil {
			return nil, args.ProjectID, err
		}
		value, err := service.Comments(ctx, args.EndpointID, args.FieldID)
		return value, args.ProjectID, err
	default:
		return nil, args.ProjectID, toolInputError("unknown tool")
	}
}

func requireProject(projectID, workspaceID string) error {
	if projectID == "" {
		return toolInputError("validation failed: project_id is required")
	}
	if projectID != workspaceID {
		return toolPermissionError("project is outside the authenticated workspace")
	}
	return nil
}

func projectSummary(ws *entity.Workspace) map[string]any {
	return map[string]any{"id": ws.ID, "revision": ws.Rev, "resource_count": len(ws.Resources)}
}

type publicToolError struct{ message string }

func (e *publicToolError) Error() string { return e.message }

func toolInputError(message string) error { return &publicToolError{message: message} }
func toolPermissionError(message string) error {
	return &publicToolError{message: "unauthorized: " + message}
}
func toolNotFoundError(message string) error { return &publicToolError{message: message} }

func toolErrorResult(err error) map[string]any {
	message := "internal server error"
	var public *publicToolError
	switch {
	case errors.As(err, &public):
		message = public.Error()
	case errors.Is(err, usecase.ErrNotFound), errors.Is(err, usecase.ErrValidation),
		errors.Is(err, usecase.ErrForbidden), errors.Is(err, usecase.ErrRevConflict),
		errors.Is(err, port.ErrWorkspaceNotFound), errors.Is(err, port.ErrFlowNotFound):
		message = err.Error()
	}
	return map[string]any{
		"isError": true,
		"content": []map[string]string{{"type": "text", "text": message}},
	}
}

func openAPISpec(projectID string, endpoints []entity.Resource) map[string]any {
	paths := map[string]any{}
	for _, endpoint := range endpoints {
		if endpoint.Method == nil || endpoint.Path == nil {
			continue
		}
		pathItem, _ := paths[*endpoint.Path].(map[string]any)
		if pathItem == nil {
			pathItem = map[string]any{}
			paths[*endpoint.Path] = pathItem
		}
		pathItem[strings.ToLower(*endpoint.Method)] = map[string]any{
			"operationId": endpoint.Name,
			"responses": map[string]any{"200": map[string]any{
				"description": "Successful response",
				"content":     map[string]any{"application/json": map[string]any{"schema": jsonSchema(endpoint)}},
			}},
		}
	}
	return map[string]any{
		"openapi": "3.1.0",
		"info":    map[string]any{"title": fmt.Sprintf("Fark Noi project %s", projectID), "version": "1.0.0"},
		"paths":   paths,
	}
}

func jsonSchema(resource entity.Resource) map[string]any {
	properties := map[string]any{}
	required := make([]string, 0)
	for _, field := range resource.Fields {
		if field.Change == entity.ChangeRemoved {
			continue
		}
		property := map[string]any{"type": schemaType(field.Type)}
		if field.Description != nil {
			property["description"] = *field.Description
		}
		if field.Value != nil {
			property["example"] = field.Value
		}
		properties[field.Key] = property
		if field.Required {
			required = append(required, field.Key)
		}
	}
	result := map[string]any{
		"$schema":              "https://json-schema.org/draft/2020-12/schema",
		"title":                resource.Name,
		"type":                 "object",
		"properties":           properties,
		"additionalProperties": false,
	}
	if len(required) > 0 {
		result["required"] = required
	}
	return result
}

func schemaType(fieldType string) any {
	switch fieldType {
	case "number":
		return "number"
	case "boolean":
		return "boolean"
	case "json":
		return []string{"object", "array", "string", "number", "boolean", "null"}
	case "string[]":
		return "array"
	case "number[]":
		return "array"
	default:
		return "string"
	}
}
