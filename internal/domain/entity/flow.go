package entity

import "time"

// FlowVariable is an input to a workflow (or a workflow-level constant).
// In mirrors the Arazzo location hint ("query", "path", "header", "body", ...).
type FlowVariable struct {
	Name  string
	In    string
	Value any
}

// FlowOutput captures a value from a step's response so later steps can consume
// it. From is an expression against the response (e.g. "$response.body#/id").
type FlowOutput struct {
	Name string
	From string
}

// StepParameter is a request parameter contributed by a step. In is one of
// "query" | "path" | "header". Value may reference variables via {name} / $expr.
type StepParameter struct {
	Name  string
	In    string
	Value any
}

// Criterion is an Arazzo successCriteria entry — a condition the response must
// satisfy for the step to pass. Type is "" (simple), "jsonpath", "regex", etc.
type Criterion struct {
	Condition string
	Context   string
	Type      string
}

// FlowStep is one HTTP call in a workflow.
type FlowStep struct {
	ID              string
	StepID          string // Arazzo stepId (stable label within the flow)
	Description     string
	OperationID     string // resolved against workspace endpoint resources when Method/Path absent
	Method          string
	Path            string
	Order           int
	DependsOn       []string
	Parameters      []StepParameter
	RequestBody     any
	Outputs         []FlowOutput
	SuccessCriteria []Criterion
}

// FlowDefinition is a parsed + (optionally) persisted workflow.
type FlowDefinition struct {
	ID          string
	WorkspaceID string
	Name        string
	Description string
	Inputs      []FlowVariable
	Steps       []FlowStep
	CreatedAt   time.Time
	CreatedBy   string
}

// StepResult is the outcome of executing one step during a run.
type StepResult struct {
	StepID      string
	Method      string
	URL         string
	Status      int
	DurationMs  int64
	Passed      bool
	Skipped     bool
	Failures    []string
	Outputs     map[string]any
	Error       string
	RequestBody string
	Response    string
}

// FlowRunStatus is the terminal state of a run.
type FlowRunStatus string

const (
	RunPassed  FlowRunStatus = "passed"
	RunFailed  FlowRunStatus = "failed"
	RunErrored FlowRunStatus = "errored"
)

// FlowRun is a single execution of a workflow and its per-step results.
type FlowRun struct {
	ID          string
	FlowID      string
	WorkspaceID string
	Status      FlowRunStatus
	BaseURL     string
	StartedAt   time.Time
	FinishedAt  time.Time
	Steps       []StepResult
}
