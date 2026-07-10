package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"kingdom_manager/backend/internal/adapter/http/middleware"
	"kingdom_manager/backend/internal/domain/entity"
	"kingdom_manager/backend/internal/domain/port"
	"kingdom_manager/backend/internal/usecase"
)

type workspaceRepository struct{ workspace *entity.Workspace }

func (r workspaceRepository) Get(_ context.Context, id string) (*entity.Workspace, error) {
	if r.workspace == nil || r.workspace.ID != id {
		return nil, port.ErrWorkspaceNotFound
	}
	copy := *r.workspace
	copy.Resources = append([]entity.Resource(nil), r.workspace.Resources...)
	copy.Comments = append([]entity.Comment(nil), r.workspace.Comments...)
	copy.Collaborators = append([]entity.Collaborator(nil), r.workspace.Collaborators...)
	return &copy, nil
}
func (workspaceRepository) Create(context.Context, *entity.Workspace) error         { return nil }
func (workspaceRepository) CreateIfAbsent(context.Context, *entity.Workspace) error { return nil }
func (workspaceRepository) Save(context.Context, *entity.Workspace, int64) error    { return nil }

type flowRepository struct{ flow *entity.FlowDefinition }

func (flowRepository) CreateFlow(context.Context, *entity.FlowDefinition) error { return nil }
func (r flowRepository) ListFlows(context.Context, string) ([]entity.FlowDefinition, error) {
	if r.flow == nil {
		return []entity.FlowDefinition{}, nil
	}
	return []entity.FlowDefinition{*r.flow}, nil
}
func (r flowRepository) GetFlow(_ context.Context, id string) (*entity.FlowDefinition, error) {
	if r.flow == nil || r.flow.ID != id {
		return nil, port.ErrFlowNotFound
	}
	copy := *r.flow
	return &copy, nil
}
func (flowRepository) DeleteFlow(context.Context, string, string) (bool, error) { return false, nil }
func (flowRepository) SaveRun(context.Context, *entity.FlowRun) error           { return nil }
func (flowRepository) ListRuns(context.Context, string) ([]entity.FlowRun, error) {
	return nil, nil
}
func (flowRepository) GetRun(context.Context, string) (*entity.FlowRun, error) {
	return nil, port.ErrFlowRunNotFound
}

func testRouter(t *testing.T, enabled bool) (*gin.Engine, *middleware.Auth) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	method, path := "GET", "/users"
	ws := &entity.Workspace{
		ID: "wsp_test", Rev: 2,
		Collaborators: []entity.Collaborator{{ID: "col_allowed", Name: "Allowed", Role: entity.RoleBackend}},
		Resources: []entity.Resource{{
			ID: "res_users", Name: "listUsers", Kind: entity.KindEndpoint,
			Method: &method, Path: &path,
			Fields: []entity.SchemaField{{ID: "fld_id", Key: "id", Type: "uuid", Required: true}},
		}},
		Comments: []entity.Comment{{ID: "cmt_one", ResourceID: "res_users", AuthorID: "col_allowed", Body: "Review this"}},
	}
	workspaceService := usecase.NewService(workspaceRepository{workspace: ws}, nil, nil, "wsp_test", nil)
	flowService := usecase.NewFlowService(flowRepository{flow: &entity.FlowDefinition{
		ID: "flw_test", WorkspaceID: "wsp_test", Name: "Test flow",
	}}, workspaceService, nil, nil)
	auth := middleware.NewAuth(strings.Repeat("s", 32), time.Hour)
	router := gin.New()
	router.GET("/existing", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	server := NewServer(workspaceService, flowService, slog.New(slog.NewTextHandler(io.Discard, nil)))
	Mount(router, enabled, "/mcp", auth, server)
	return router, auth
}

func call(t *testing.T, router http.Handler, token string, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	return recorder
}

func toolCall(name, args string) string {
	return `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"` + name + `","arguments":` + args + `}}`
}

func TestAuthenticatedToolUsesWorkspaceService(t *testing.T) {
	router, auth := testRouter(t, true)
	token, err := auth.Issue("col_allowed", "wsp_test")
	if err != nil {
		t.Fatal(err)
	}

	response := call(t, router, token, toolCall("listEndpoints", `{"project_id":"wsp_test"}`))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "res_users") {
		t.Fatalf("tool result does not contain endpoint: %s", response.Body.String())
	}
}

func TestMCPRejectsMissingAuthentication(t *testing.T) {
	router, _ := testRouter(t, true)

	response := call(t, router, "", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", response.Code)
	}
}

func TestMCPRejectsUserOutsideWorkspace(t *testing.T) {
	router, auth := testRouter(t, true)
	token, err := auth.Issue("col_unknown", "wsp_test")
	if err != nil {
		t.Fatal(err)
	}

	response := call(t, router, token, toolCall("listEndpoints", `{"project_id":"wsp_test"}`))

	var envelope struct {
		Result struct {
			IsError bool `json:"isError"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if !envelope.Result.IsError || len(envelope.Result.Content) == 0 || !strings.Contains(envelope.Result.Content[0].Text, "unauthorized") {
		t.Fatalf("expected permission error, body = %s", response.Body.String())
	}
}

func TestMCPRejectsCrossWorkspaceProject(t *testing.T) {
	router, auth := testRouter(t, true)
	token, err := auth.Issue("col_allowed", "wsp_test")
	if err != nil {
		t.Fatal(err)
	}

	response := call(t, router, token, toolCall("getProject", `{"project_id":"wsp_other"}`))

	if !strings.Contains(response.Body.String(), "outside the authenticated workspace") {
		t.Fatalf("expected project permission error, body = %s", response.Body.String())
	}
}

func TestDisabledMCPDoesNotAffectExistingRoutes(t *testing.T) {
	router, _ := testRouter(t, false)

	existing := httptest.NewRecorder()
	router.ServeHTTP(existing, httptest.NewRequest(http.MethodGet, "/existing", nil))
	if existing.Code != http.StatusNoContent {
		t.Fatalf("existing route status = %d", existing.Code)
	}
	mcpResponse := call(t, router, "", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	if mcpResponse.Code != http.StatusNotFound {
		t.Fatalf("disabled MCP status = %d, want 404", mcpResponse.Code)
	}
}
