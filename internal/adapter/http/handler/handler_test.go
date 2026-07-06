package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"kingdom_manager/backend/internal/adapter/http/middleware"
	"kingdom_manager/backend/internal/domain/entity"
	"kingdom_manager/backend/internal/usecase"
)

// memRepo is a minimal in-memory WorkspaceRepository for HTTP-layer tests.
type memRepo struct{ ws *entity.Workspace }

func (m *memRepo) Get(context.Context, string) (*entity.Workspace, error) {
	copy := *m.ws
	copy.Resources = append([]entity.Resource(nil), m.ws.Resources...)
	return &copy, nil
}
func (m *memRepo) Create(context.Context, *entity.Workspace) error         { return nil }
func (m *memRepo) CreateIfAbsent(context.Context, *entity.Workspace) error { return nil }
func (m *memRepo) Save(_ context.Context, ws *entity.Workspace, _ int64) error {
	m.ws = ws
	return nil
}

func TestImportResourcesHandlerCreatesEndpoints(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &memRepo{ws: &entity.Workspace{
		ID: "wsp_test", Rev: 5,
		Collaborators: []entity.Collaborator{{ID: "col_test", Name: "Tester"}},
	}}
	h := New(usecase.NewService(repo, "wsp_test", nil), nil, nil, nil, nil)

	body := `{"endpoints":[
		{"name":"getUser","method":"get","path":"/users/{id}",
		 "fields":[{"key":"expand","type":"string","required":false}],
		 "responses":[{"status":200,"fields":[{"id":"rsf_1","key":"id","type":"uuid","required":true,"state":"ready","change":"added"}]}]},
		{"name":"createUser","method":"POST","path":"/users"}
	]}`

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/resources/import", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(middleware.CollaboratorKey, "col_test")
	c.Set(middleware.WorkspaceKey, "wsp_test")

	h.ImportResources(c)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d — body: %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Data struct {
			Rev       int64 `json:"rev"`
			Resources []struct {
				Method    string `json:"method"`
				Fields    []any  `json:"fields"`
				Responses []any  `json:"responses"`
			} `json:"resources"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Data.Rev != 6 || len(out.Data.Resources) != 2 {
		t.Fatalf("unexpected result: rev=%d resources=%d", out.Data.Rev, len(out.Data.Resources))
	}
	if out.Data.Resources[0].Method != "GET" {
		t.Fatalf("method not upper-cased: %q", out.Data.Resources[0].Method)
	}
	// No seeded id field — only the imported request field.
	if len(out.Data.Resources[0].Fields) != 1 {
		t.Fatalf("expected 1 field, got %d", len(out.Data.Resources[0].Fields))
	}
	if len(out.Data.Resources[0].Responses) != 1 {
		t.Fatalf("expected 1 response schema, got %d", len(out.Data.Resources[0].Responses))
	}
}

func TestUpdateFieldRequestDistinguishesMissingAndNullValue(t *testing.T) {
	var missing updateFieldRequest
	if err := json.Unmarshal([]byte(`{"state":"ready"}`), &missing); err != nil {
		t.Fatal(err)
	}
	if missing.Value.Set {
		t.Fatal("missing value was marked as set")
	}

	var null updateFieldRequest
	if err := json.Unmarshal([]byte(`{"value":null}`), &null); err != nil {
		t.Fatal(err)
	}
	if !null.Value.Set || null.Value.Value != nil {
		t.Fatalf("expected explicit null, got set=%v value=%#v", null.Value.Set, null.Value.Value)
	}

	var nested updateFieldRequest
	if err := json.Unmarshal([]byte(`{"value":{"profile":{"active":true},"tags":["new"]}}`), &nested); err != nil {
		t.Fatal(err)
	}
	if !nested.Value.Set {
		t.Fatal("nested value was not marked as set")
	}
}

func TestReplaceResponsesRequestDistinguishesMissingAndEmpty(t *testing.T) {
	var missing replaceResponsesRequest
	if err := json.Unmarshal([]byte(`{}`), &missing); err != nil {
		t.Fatal(err)
	}
	if missing.Responses != nil {
		t.Fatalf("missing responses was set: %#v", missing.Responses)
	}

	var empty replaceResponsesRequest
	if err := json.Unmarshal([]byte(`{"responses":[]}`), &empty); err != nil {
		t.Fatal(err)
	}
	if empty.Responses == nil || *empty.Responses == nil || len(*empty.Responses) != 0 {
		t.Fatalf("expected explicit empty responses, got %#v", empty.Responses)
	}
}

func TestRevisionConflictIncludesCurrentWorkspaceSnapshot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &memRepo{ws: &entity.Workspace{
		ID: "wsp_test", Rev: 5,
		Collaborators: []entity.Collaborator{{ID: "col_test", Name: "Tester"}},
		Resources: []entity.Resource{{
			ID: "res_test", Name: "User", Kind: entity.KindModel, Fields: []entity.SchemaField{},
		}},
	}}
	h := New(usecase.NewService(repo, "wsp_test", nil), nil, nil, nil, nil)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodDelete, "/api/v1/resources/res_test", nil)
	c.Request.Header.Set("If-Match", "4")
	c.Set(middleware.CollaboratorKey, "col_test")
	c.Set(middleware.WorkspaceKey, "wsp_test")

	h.DeleteResource(c)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
	var response struct {
		Data struct {
			Rev         int64  `json:"rev"`
			WorkspaceID string `json:"workspace_id"`
		} `json:"data"`
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Error.Code != "REV_CONFLICT" || response.Data.Rev != 5 || response.Data.WorkspaceID != "wsp_test" {
		t.Fatalf("unexpected conflict response: %+v", response)
	}
}
