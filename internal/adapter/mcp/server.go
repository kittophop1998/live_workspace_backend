package mcp

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"kingdom_manager/backend/internal/adapter/http/middleware"
	"kingdom_manager/backend/internal/usecase"
)

const protocolVersion = "2025-03-26"

type Server struct {
	workspaces *usecase.Service
	flows      *usecase.FlowService
	logger     *slog.Logger
}

func NewServer(workspaces *usecase.Service, flows *usecase.FlowService, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{workspaces: workspaces, flows: flows, logger: logger}
}

func Mount(router *gin.Engine, enabled bool, path string, auth *middleware.Auth, server *Server) {
	if !enabled {
		return
	}
	router.POST(path, auth.Handler(), server.Handle)
}

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (s *Server) Handle(c *gin.Context) {
	var req request
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20))
	if err := decoder.Decode(&req); err != nil || req.JSONRPC != "2.0" || req.Method == "" {
		c.JSON(http.StatusBadRequest, response{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32700, Message: "invalid JSON-RPC request"}})
		return
	}

	switch req.Method {
	case "initialize":
		c.JSON(http.StatusOK, response{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":      map[string]any{"name": "fark-noi", "version": "1.0.0"},
		}})
	case "notifications/initialized":
		c.Status(http.StatusAccepted)
	case "ping":
		c.JSON(http.StatusOK, response{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{}})
	case "tools/list":
		c.JSON(http.StatusOK, response{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{"tools": toolDefinitions()}})
	case "tools/call":
		s.callTool(c, req)
	default:
		c.JSON(http.StatusOK, response{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32601, Message: "method not found"}})
	}
}

func (s *Server) callTool(c *gin.Context, req request) {
	started := time.Now()
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil || params.Name == "" {
		c.JSON(http.StatusOK, response{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32602, Message: "invalid tool call parameters"}})
		return
	}
	if len(params.Arguments) == 0 {
		params.Arguments = json.RawMessage(`{}`)
	}

	workspaceID := middleware.WorkspaceID(c)
	userID := middleware.CollaboratorID(c)
	result, projectID, err := s.execute(c.Request.Context(), workspaceID, userID, params.Name, params.Arguments)
	s.logger.Info("MCP tool request",
		"tool", params.Name,
		"user_id", userID,
		"workspace_id", workspaceID,
		"project_id", projectID,
		"duration_ms", time.Since(started).Milliseconds(),
		"error", err != nil,
	)
	if err != nil {
		c.JSON(http.StatusOK, response{JSONRPC: "2.0", ID: req.ID, Result: toolErrorResult(err)})
		return
	}
	payload, err := json.Marshal(result)
	if err != nil {
		c.JSON(http.StatusOK, response{JSONRPC: "2.0", ID: req.ID, Result: toolErrorResult(errors.New("unable to encode tool result"))})
		return
	}
	c.JSON(http.StatusOK, response{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{
		"content": []map[string]string{{"type": "text", "text": string(payload)}},
	}})
}
