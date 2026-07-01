package handler

import (
	"time"

	"kingdom_manager/backend/internal/domain/entity"
)

// ---- Flow definition wire shapes (snake_case) ----------------------------

type flowVariableBody struct {
	Name  string `json:"name"`
	In    string `json:"in"`
	Value any    `json:"value,omitempty"`
}
type flowOutputBody struct {
	Name string `json:"name"`
	From string `json:"from"`
}
type stepParameterBody struct {
	Name  string `json:"name"`
	In    string `json:"in"`
	Value any    `json:"value,omitempty"`
}
type criterionBody struct {
	Condition string `json:"condition"`
	Context   string `json:"context,omitempty"`
	Type      string `json:"type,omitempty"`
}
type flowStepBody struct {
	ID              string              `json:"id,omitempty"`
	StepID          string              `json:"step_id"`
	Description     string              `json:"description,omitempty"`
	OperationID     string              `json:"operation_id,omitempty"`
	Method          string              `json:"method,omitempty"`
	Path            string              `json:"path,omitempty"`
	Order           int                 `json:"order"`
	DependsOn       []string            `json:"depends_on"`
	Parameters      []stepParameterBody `json:"parameters"`
	RequestBody     any                 `json:"request_body,omitempty"`
	Outputs         []flowOutputBody    `json:"outputs"`
	SuccessCriteria []criterionBody     `json:"success_criteria"`
}

// flowRequest is the payload the frontend sends back to persist a parsed flow.
type flowRequest struct {
	Name        string             `json:"name" binding:"required"`
	Description string             `json:"description"`
	Inputs      []flowVariableBody `json:"inputs"`
	Steps       []flowStepBody     `json:"steps" binding:"required"`
}

func (r flowRequest) toEntity() entity.FlowDefinition {
	flow := entity.FlowDefinition{Name: r.Name, Description: r.Description}
	for _, input := range r.Inputs {
		flow.Inputs = append(flow.Inputs, entity.FlowVariable{Name: input.Name, In: input.In, Value: input.Value})
	}
	for _, step := range r.Steps {
		item := entity.FlowStep{
			StepID: step.StepID, Description: step.Description, OperationID: step.OperationID,
			Method: step.Method, Path: step.Path, Order: step.Order, DependsOn: step.DependsOn,
			RequestBody: step.RequestBody,
		}
		for _, param := range step.Parameters {
			item.Parameters = append(item.Parameters, entity.StepParameter{Name: param.Name, In: param.In, Value: param.Value})
		}
		for _, output := range step.Outputs {
			item.Outputs = append(item.Outputs, entity.FlowOutput{Name: output.Name, From: output.From})
		}
		for _, criterion := range step.SuccessCriteria {
			item.SuccessCriteria = append(item.SuccessCriteria, entity.Criterion{Condition: criterion.Condition, Context: criterion.Context, Type: criterion.Type})
		}
		flow.Steps = append(flow.Steps, item)
	}
	return flow
}

type flowResponse struct {
	ID          string             `json:"id,omitempty"`
	WorkspaceID string             `json:"workspace_id,omitempty"`
	Name        string             `json:"name"`
	Description string             `json:"description"`
	Inputs      []flowVariableBody `json:"inputs"`
	Steps       []flowStepBody     `json:"steps"`
	CreatedAt   *time.Time         `json:"created_at,omitempty"`
	CreatedBy   string             `json:"created_by,omitempty"`
}

func flowDTO(flow *entity.FlowDefinition) flowResponse {
	out := flowResponse{
		ID: flow.ID, WorkspaceID: flow.WorkspaceID, Name: flow.Name, Description: flow.Description,
		CreatedBy: flow.CreatedBy, Inputs: []flowVariableBody{}, Steps: []flowStepBody{},
	}
	if !flow.CreatedAt.IsZero() {
		out.CreatedAt = &flow.CreatedAt
	}
	for _, input := range flow.Inputs {
		out.Inputs = append(out.Inputs, flowVariableBody{Name: input.Name, In: input.In, Value: input.Value})
	}
	for _, step := range flow.Steps {
		item := flowStepBody{
			ID: step.ID, StepID: step.StepID, Description: step.Description, OperationID: step.OperationID,
			Method: step.Method, Path: step.Path, Order: step.Order, DependsOn: emptyIfNil(step.DependsOn),
			RequestBody: step.RequestBody, Parameters: []stepParameterBody{}, Outputs: []flowOutputBody{},
			SuccessCriteria: []criterionBody{},
		}
		for _, param := range step.Parameters {
			item.Parameters = append(item.Parameters, stepParameterBody{Name: param.Name, In: param.In, Value: param.Value})
		}
		for _, output := range step.Outputs {
			item.Outputs = append(item.Outputs, flowOutputBody{Name: output.Name, From: output.From})
		}
		for _, criterion := range step.SuccessCriteria {
			item.SuccessCriteria = append(item.SuccessCriteria, criterionBody{Condition: criterion.Condition, Context: criterion.Context, Type: criterion.Type})
		}
		out.Steps = append(out.Steps, item)
	}
	return out
}

// ---- Flow run wire shapes -------------------------------------------------

type stepResultResponse struct {
	StepID      string         `json:"step_id"`
	Method      string         `json:"method"`
	URL         string         `json:"url"`
	Status      int            `json:"status"`
	DurationMs  int64          `json:"duration_ms"`
	Passed      bool           `json:"passed"`
	Skipped     bool           `json:"skipped"`
	Failures    []string       `json:"failures"`
	Outputs     map[string]any `json:"outputs"`
	Error       string         `json:"error,omitempty"`
	RequestBody string         `json:"request_body,omitempty"`
	Response    string         `json:"response,omitempty"`
}
type runResponse struct {
	ID          string               `json:"id"`
	FlowID      string               `json:"flow_id"`
	WorkspaceID string               `json:"workspace_id"`
	Status      string               `json:"status"`
	BaseURL     string               `json:"base_url"`
	StartedAt   time.Time            `json:"started_at"`
	FinishedAt  time.Time            `json:"finished_at"`
	Steps       []stepResultResponse `json:"steps"`
}

func runDTO(run *entity.FlowRun) runResponse {
	out := runResponse{
		ID: run.ID, FlowID: run.FlowID, WorkspaceID: run.WorkspaceID, Status: string(run.Status),
		BaseURL: run.BaseURL, StartedAt: run.StartedAt, FinishedAt: run.FinishedAt, Steps: []stepResultResponse{},
	}
	for _, step := range run.Steps {
		out.Steps = append(out.Steps, stepResultResponse{
			StepID: step.StepID, Method: step.Method, URL: step.URL, Status: step.Status,
			DurationMs: step.DurationMs, Passed: step.Passed, Skipped: step.Skipped,
			Failures: emptyIfNil(step.Failures), Outputs: step.Outputs, Error: step.Error,
			RequestBody: step.RequestBody, Response: step.Response,
		})
	}
	return out
}

func emptyIfNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
