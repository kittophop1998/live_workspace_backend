package usecase

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"kingdom_manager/backend/internal/arazzo"
	"kingdom_manager/backend/internal/domain/entity"
	"kingdom_manager/backend/internal/domain/port"
	"kingdom_manager/backend/internal/httpexec"
)

// fakeFlowRepo is an in-memory port.FlowRepository for tests.
type fakeFlowRepo struct {
	flows map[string]*entity.FlowDefinition
	runs  map[string]*entity.FlowRun
}

func newFakeFlowRepo() *fakeFlowRepo {
	return &fakeFlowRepo{flows: map[string]*entity.FlowDefinition{}, runs: map[string]*entity.FlowRun{}}
}
func (r *fakeFlowRepo) CreateFlow(_ context.Context, flow *entity.FlowDefinition) error {
	r.flows[flow.ID] = flow
	return nil
}
func (r *fakeFlowRepo) ListFlows(_ context.Context, workspaceID string) ([]entity.FlowDefinition, error) {
	out := []entity.FlowDefinition{}
	for _, flow := range r.flows {
		if flow.WorkspaceID == workspaceID {
			out = append(out, *flow)
		}
	}
	return out, nil
}
func (r *fakeFlowRepo) GetFlow(_ context.Context, id string) (*entity.FlowDefinition, error) {
	if flow, ok := r.flows[id]; ok {
		return flow, nil
	}
	return nil, port.ErrFlowNotFound
}
func (r *fakeFlowRepo) SaveRun(_ context.Context, run *entity.FlowRun) error {
	r.runs[run.ID] = run
	return nil
}
func (r *fakeFlowRepo) ListRuns(_ context.Context, flowID string) ([]entity.FlowRun, error) {
	out := []entity.FlowRun{}
	for _, run := range r.runs {
		if run.FlowID == flowID {
			out = append(out, *run)
		}
	}
	return out, nil
}
func (r *fakeFlowRepo) GetRun(_ context.Context, runID string) (*entity.FlowRun, error) {
	if run, ok := r.runs[runID]; ok {
		return run, nil
	}
	return nil, port.ErrFlowRunNotFound
}

func chainingFlow() *entity.FlowDefinition {
	return &entity.FlowDefinition{
		ID: "flw_1", WorkspaceID: "ws1", Name: "loginAndFetch",
		Steps: []entity.FlowStep{
			{
				StepID: "login", Method: "POST", Path: "/login",
				RequestBody:     map[string]any{"user": "$inputs.user"},
				Outputs:         []entity.FlowOutput{{Name: "token", From: "$response.body#/token"}},
				SuccessCriteria: []entity.Criterion{{Condition: "$statusCode == 200"}},
			},
			{
				StepID: "profile", Method: "GET", Path: "/profile", DependsOn: []string{"login"},
				Parameters: []entity.StepParameter{{Name: "Authorization", In: "header", Value: "$steps.login.outputs.token"}},
				SuccessCriteria: []entity.Criterion{
					{Condition: "$statusCode == 200"},
					{Context: "$response.body#/name", Condition: "^alice$", Type: "regex"},
				},
			},
		},
	}
}

func TestRunChainsOutputsAndValidates(t *testing.T) {
	var gotAuth, gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login":
			data, _ := io.ReadAll(r.Body)
			gotBody = string(data)
			_, _ = w.Write([]byte(`{"token":"abc123"}`))
		case "/profile":
			gotAuth = r.Header.Get("Authorization")
			_, _ = w.Write([]byte(`{"name":"alice"}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()

	repo := newFakeFlowRepo()
	repo.flows["flw_1"] = chainingFlow()
	service := NewFlowService(repo, nil, arazzo.Parser{}, httpexec.New(true))

	run, err := service.Run(context.Background(), "ws1", "flw_1", RunInput{BaseURL: server.URL, Inputs: map[string]any{"user": "bob"}})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if run.Status != entity.RunPassed {
		t.Fatalf("status = %s, steps = %+v", run.Status, run.Steps)
	}
	if gotBody != `{"user":"bob"}` {
		t.Errorf("login body not interpolated: %q", gotBody)
	}
	if run.Steps[0].Outputs["token"] != "abc123" {
		t.Errorf("token output = %v", run.Steps[0].Outputs["token"])
	}
	if gotAuth != "abc123" {
		t.Errorf("token not chained into profile Authorization header: %q", gotAuth)
	}
	if !run.Steps[1].Passed {
		t.Errorf("profile step failed: %+v", run.Steps[1])
	}
}

func TestRunStopsAndSkipsAfterFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(200)
	}))
	defer server.Close()

	repo := newFakeFlowRepo()
	repo.flows["flw_1"] = chainingFlow()
	service := NewFlowService(repo, nil, arazzo.Parser{}, httpexec.New(true))

	run, err := service.Run(context.Background(), "ws1", "flw_1", RunInput{BaseURL: server.URL, Inputs: map[string]any{"user": "bob"}})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if run.Status != entity.RunFailed {
		t.Errorf("status = %s", run.Status)
	}
	if run.Steps[0].Passed {
		t.Error("login should have failed on 500")
	}
	if !run.Steps[1].Skipped {
		t.Error("profile should be skipped after login failed")
	}
}

func TestRunRequiresBaseURL(t *testing.T) {
	repo := newFakeFlowRepo()
	repo.flows["flw_1"] = chainingFlow()
	service := NewFlowService(repo, nil, arazzo.Parser{}, httpexec.New(true))
	if _, err := service.Run(context.Background(), "ws1", "flw_1", RunInput{}); err == nil {
		t.Error("want validation error for missing base_url")
	}
	if _, err := service.Run(context.Background(), "ws1", "flw_1", RunInput{BaseURL: "localhost:8080"}); err == nil {
		t.Error("want validation error for non-absolute base_url")
	}
}

func TestSaveRejectsInvalidStepGraph(t *testing.T) {
	service := NewFlowService(newFakeFlowRepo(), nil, arazzo.Parser{}, httpexec.New(true))
	tests := []struct {
		name  string
		steps []entity.FlowStep
	}{
		{"missing step id", []entity.FlowStep{{Path: "/one"}}},
		{"duplicate step id", []entity.FlowStep{{StepID: "one"}, {StepID: "one"}}},
		{"unknown dependency", []entity.FlowStep{{StepID: "one", DependsOn: []string{"missing"}}}},
		{"dependency cycle", []entity.FlowStep{
			{StepID: "one", DependsOn: []string{"two"}},
			{StepID: "two", DependsOn: []string{"one"}},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := service.Save(context.Background(), "ws1", "actor", entity.FlowDefinition{Name: "flow", Steps: tt.steps})
			if err == nil {
				t.Fatal("want validation error")
			}
		})
	}
}

func TestUnsupportedCriterionFails(t *testing.T) {
	passed, failures := evaluateCriteria(
		[]entity.Criterion{{Condition: "$statusCode"}},
		httpexec.Response{Status: http.StatusOK},
		nil,
	)
	if passed || len(failures) != 1 {
		t.Fatalf("passed=%v failures=%v", passed, failures)
	}
}
