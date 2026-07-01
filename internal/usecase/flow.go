package usecase

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"kingdom_manager/backend/internal/domain/entity"
	"kingdom_manager/backend/internal/domain/port"
)

// FlowService owns E2E workflow parsing, persistence, and execution. It reuses
// the workspace Service to resolve a step's operationId to a real method+path
// from the shared API spec — the integration point that ties flows to the specs
// devs are already collaborating on.
type FlowService struct {
	repo      port.FlowRepository
	workspace *Service
	parser    port.FlowParser
	exec      port.HTTPExecutor
	now       func() time.Time
}

func NewFlowService(repo port.FlowRepository, workspace *Service, parser port.FlowParser, exec port.HTTPExecutor) *FlowService {
	return &FlowService{repo: repo, workspace: workspace, parser: parser, exec: exec, now: func() time.Time { return time.Now().UTC() }}
}

// Parse turns an uploaded Arazzo document into flow definitions (not persisted).
func (s *FlowService) Parse(data []byte) ([]entity.FlowDefinition, error) {
	flows, err := s.parser.Parse(data)
	if err != nil {
		return nil, validation(err.Error(), nil)
	}
	return flows, nil
}

// Save persists a parsed flow, assigning stable IDs.
func (s *FlowService) Save(ctx context.Context, workspaceID, actorName string, in entity.FlowDefinition) (*entity.FlowDefinition, error) {
	if strings.TrimSpace(in.Name) == "" {
		return nil, validation("flow name is required", nil)
	}
	if len(in.Steps) == 0 {
		return nil, validation("flow must have at least one step", nil)
	}
	if _, err := orderSteps(in.Steps); err != nil {
		return nil, validation(err.Error(), nil)
	}
	flow := in
	flow.ID = "flw_" + shortID()
	flow.WorkspaceID = workspaceID
	flow.CreatedAt = s.now()
	flow.CreatedBy = actorName
	for i := range flow.Steps {
		flow.Steps[i].ID = "fst_" + shortID()
		flow.Steps[i].Order = i
	}
	if err := s.repo.CreateFlow(ctx, &flow); err != nil {
		return nil, fmt.Errorf("save flow: %w", err)
	}
	return &flow, nil
}

func (s *FlowService) List(ctx context.Context, workspaceID string) ([]entity.FlowDefinition, error) {
	return s.repo.ListFlows(ctx, workspaceID)
}

func (s *FlowService) Get(ctx context.Context, workspaceID, id string) (*entity.FlowDefinition, error) {
	flow, err := s.repo.GetFlow(ctx, id)
	if err != nil {
		if errors.Is(err, port.ErrFlowNotFound) {
			return nil, notFound("flow", id)
		}
		return nil, err
	}
	if flow.WorkspaceID != workspaceID {
		return nil, notFound("flow", id)
	}
	return flow, nil
}

// Delete hard-deletes a room-scoped flow and its persisted run history.
func (s *FlowService) Delete(ctx context.Context, workspaceID, id string) error {
	deleted, err := s.repo.DeleteFlow(ctx, workspaceID, id)
	if err != nil {
		return fmt.Errorf("delete flow: %w", err)
	}
	if !deleted {
		return notFound("flow", id)
	}
	return nil
}

func (s *FlowService) ListRuns(ctx context.Context, workspaceID, flowID string) ([]entity.FlowRun, error) {
	if _, err := s.Get(ctx, workspaceID, flowID); err != nil {
		return nil, err
	}
	return s.repo.ListRuns(ctx, flowID)
}

func (s *FlowService) GetRun(ctx context.Context, workspaceID, runID string) (*entity.FlowRun, error) {
	run, err := s.repo.GetRun(ctx, runID)
	if err != nil {
		if errors.Is(err, port.ErrFlowRunNotFound) {
			return nil, notFound("flow run", runID)
		}
		return nil, err
	}
	if run.WorkspaceID != workspaceID {
		return nil, notFound("flow run", runID)
	}
	return run, nil
}

// RunInput carries the target server + caller-supplied input variables.
type RunInput struct {
	BaseURL string
	Inputs  map[string]any
}

// Run executes a saved flow's steps in dependency order against BaseURL,
// chaining outputs into later steps, evaluating success criteria, and persisting
// the result. The first failing/erroring step stops the run; the rest are marked
// skipped.
func (s *FlowService) Run(ctx context.Context, workspaceID string, flowID string, in RunInput) (*entity.FlowRun, error) {
	flow, err := s.Get(ctx, workspaceID, flowID)
	if err != nil {
		return nil, err
	}
	baseURL := strings.TrimRight(strings.TrimSpace(in.BaseURL), "/")
	if baseURL == "" {
		return nil, validation("base_url is required to run a flow", nil)
	}
	parsedBaseURL, err := url.Parse(baseURL)
	if err != nil || parsedBaseURL.Host == "" || (parsedBaseURL.Scheme != "http" && parsedBaseURL.Scheme != "https") {
		return nil, validation("base_url must be an absolute HTTP or HTTPS URL", nil)
	}

	ordered, err := orderSteps(flow.Steps)
	if err != nil {
		return nil, validation(err.Error(), nil)
	}
	operations := s.endpointOperations(ctx, workspaceID)

	run := &entity.FlowRun{
		ID: "run_" + shortID(), FlowID: flow.ID, WorkspaceID: workspaceID,
		BaseURL: baseURL, StartedAt: s.now(), Status: entity.RunPassed,
	}
	rc := &runContext{inputs: mergeInputs(flow.Inputs, in.Inputs), steps: map[string]map[string]any{}}

	stopped := false
	for _, step := range ordered {
		if stopped {
			run.Steps = append(run.Steps, entity.StepResult{StepID: step.StepID, Skipped: true})
			continue
		}
		result := s.runStep(ctx, baseURL, step, operations, rc)
		run.Steps = append(run.Steps, result)
		if result.Error != "" {
			run.Status, stopped = entity.RunErrored, true
		} else if !result.Passed {
			run.Status, stopped = entity.RunFailed, true
		}
	}

	run.FinishedAt = s.now()
	if err := s.repo.SaveRun(ctx, run); err != nil {
		return nil, fmt.Errorf("save flow run: %w", err)
	}
	return run, nil
}

// endpointOperations builds an operationId → (method, path) map from the shared
// workspace endpoint resources so steps can reference specs by name.
func (s *FlowService) endpointOperations(ctx context.Context, workspaceID string) map[string]operationRef {
	out := map[string]operationRef{}
	if s.workspace == nil {
		return out
	}
	resources, err := s.workspace.ForWorkspace(workspaceID).Resources(ctx, "endpoint")
	if err != nil {
		return out
	}
	for _, resource := range resources {
		if resource.Method == nil || resource.Path == nil {
			continue
		}
		out[strings.ToLower(resource.Name)] = operationRef{method: *resource.Method, path: *resource.Path}
	}
	return out
}

type operationRef struct{ method, path string }

// orderSteps returns steps sorted by dependency (topological), stable on the
// original order; errors on a dependency cycle.
func orderSteps(steps []entity.FlowStep) ([]entity.FlowStep, error) {
	byID := map[string]entity.FlowStep{}
	indexOf := map[string]int{}
	for i, step := range steps {
		if strings.TrimSpace(step.StepID) == "" {
			return nil, fmt.Errorf("step_id is required at index %d", i)
		}
		if _, exists := byID[step.StepID]; exists {
			return nil, fmt.Errorf("duplicate step_id %q", step.StepID)
		}
		byID[step.StepID] = step
		indexOf[step.StepID] = i
	}
	for _, step := range steps {
		for _, dep := range step.DependsOn {
			if _, exists := byID[dep]; !exists {
				return nil, fmt.Errorf("step %q depends on unknown step %q", step.StepID, dep)
			}
		}
	}
	visited := map[string]int{} // 0=unseen,1=visiting,2=done
	ordered := make([]entity.FlowStep, 0, len(steps))
	var visit func(id string) error
	visit = func(id string) error {
		switch visited[id] {
		case 2:
			return nil
		case 1:
			return fmt.Errorf("dependency cycle detected at step %q", id)
		}
		visited[id] = 1
		step := byID[id]
		deps := append([]string(nil), step.DependsOn...)
		sort.SliceStable(deps, func(i, j int) bool { return indexOf[deps[i]] < indexOf[deps[j]] })
		for _, dep := range deps {
			if err := visit(dep); err != nil {
				return err
			}
		}
		visited[id] = 2
		ordered = append(ordered, step)
		return nil
	}
	for _, step := range steps {
		if err := visit(step.StepID); err != nil {
			return nil, err
		}
	}
	return ordered, nil
}

// mergeInputs layers caller-provided inputs over the flow's declared defaults.
func mergeInputs(declared []entity.FlowVariable, provided map[string]any) map[string]any {
	out := map[string]any{}
	for _, variable := range declared {
		if variable.Value != nil {
			out[variable.Name] = variable.Value
		}
	}
	for key, value := range provided {
		out[key] = value
	}
	return out
}
