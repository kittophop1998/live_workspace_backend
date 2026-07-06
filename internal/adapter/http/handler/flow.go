package handler

import (
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"kingdom_manager/backend/internal/adapter/http/middleware"
	"kingdom_manager/backend/internal/httpexec"
	"kingdom_manager/backend/internal/usecase"
)

// ---- Single-request proxy tester -----------------------------------------

type httpTestRequest struct {
	Method  string            `json:"method"`
	URL     string            `json:"url" binding:"required"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
}

// HTTPTest proxies one outbound request (the "Try it" tester + workflow steps
// use the same executor). A transport failure is returned as a 200 envelope with
// an `error` field so the UI can render it inline rather than as an app error.
func (h *Handler) HTTPTest(c *gin.Context) {
	var request httpTestRequest
	if !bind(c, &request) {
		return
	}
	resp, err := h.executor.Exec(c.Request.Context(), httpexec.Request{
		Method: request.Method, URL: request.URL, Headers: request.Headers, Body: []byte(request.Body),
	})
	if err != nil {
		success(c, http.StatusOK, gin.H{"status": 0, "duration_ms": 0, "headers": gin.H{}, "body": "", "size": 0, "error": err.Error()})
		return
	}
	success(c, http.StatusOK, gin.H{
		"status": resp.Status, "duration_ms": resp.DurationMs, "headers": resp.Headers,
		"body": resp.Body, "size": resp.BodySize, "truncated": resp.Truncated, "error": "",
	})
}

// ---- E2E flow endpoints ---------------------------------------------------

// ParseFlow parses an uploaded Arazzo file (multipart "file" or a raw body) and
// returns the structured preview WITHOUT persisting it.
func (h *Handler) ParseFlow(c *gin.Context) {
	data, ok := h.readUpload(c)
	if !ok {
		return
	}
	flows, err := h.flowService.Parse(data)
	if err != nil {
		h.writeError(c, err)
		return
	}
	out := make([]flowResponse, 0, len(flows))
	for i := range flows {
		out = append(out, flowDTO(&flows[i]))
	}
	success(c, http.StatusOK, gin.H{"flows": out})
}

func (h *Handler) SaveFlow(c *gin.Context) {
	var request flowRequest
	if !bind(c, &request) {
		return
	}
	actor, err := h.serviceFor(c).Me(c.Request.Context(), middleware.CollaboratorID(c))
	actorName := "unknown"
	if err == nil {
		actorName = actor.Name
	}
	saved, err := h.flowService.Save(c.Request.Context(), middleware.WorkspaceID(c), actorName, request.toEntity())
	if err != nil {
		h.writeError(c, err)
		return
	}
	success(c, http.StatusCreated, flowDTO(saved))
}

func (h *Handler) ListFlows(c *gin.Context) {
	flows, err := h.flowService.List(c.Request.Context(), middleware.WorkspaceID(c))
	if err != nil {
		h.writeError(c, err)
		return
	}
	out := make([]flowResponse, 0, len(flows))
	for i := range flows {
		out = append(out, flowDTO(&flows[i]))
	}
	success(c, http.StatusOK, out)
}

func (h *Handler) GetFlow(c *gin.Context) {
	flow, err := h.flowService.Get(c.Request.Context(), middleware.WorkspaceID(c), c.Param("id"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	success(c, http.StatusOK, flowDTO(flow))
}

func (h *Handler) DeleteFlow(c *gin.Context) {
	id := c.Param("id")
	if err := h.flowService.Delete(c.Request.Context(), middleware.WorkspaceID(c), id); err != nil {
		h.writeError(c, err)
		return
	}
	success(c, http.StatusOK, gin.H{"flow_id": id})
}

type runFlowRequest struct {
	BaseURL string         `json:"base_url" binding:"required"`
	Inputs  map[string]any `json:"inputs"`
}

func (h *Handler) RunFlow(c *gin.Context) {
	var request runFlowRequest
	if !bind(c, &request) {
		return
	}
	run, err := h.flowService.Run(c.Request.Context(), middleware.WorkspaceID(c), c.Param("id"), usecase.RunInput{BaseURL: request.BaseURL, Inputs: request.Inputs})
	if err != nil {
		h.writeError(c, err)
		return
	}
	success(c, http.StatusCreated, runDTO(run))
}

func (h *Handler) ListFlowRuns(c *gin.Context) {
	runs, err := h.flowService.ListRuns(c.Request.Context(), middleware.WorkspaceID(c), c.Param("id"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	out := make([]runResponse, 0, len(runs))
	for i := range runs {
		out = append(out, runDTO(&runs[i]))
	}
	success(c, http.StatusOK, out)
}

func (h *Handler) GetFlowRun(c *gin.Context) {
	run, err := h.flowService.GetRun(c.Request.Context(), middleware.WorkspaceID(c), c.Param("run_id"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	success(c, http.StatusOK, runDTO(run))
}

// readUpload pulls raw bytes from either a multipart "file" field or the request
// body, so the frontend can send whichever is convenient.
func (h *Handler) readUpload(c *gin.Context) ([]byte, bool) {
	if file, err := c.FormFile("file"); err == nil {
		opened, err := file.Open()
		if err != nil {
			h.writeError(c, err)
			return nil, false
		}
		defer opened.Close()
		data, err := io.ReadAll(opened)
		if err != nil {
			h.writeError(c, err)
			return nil, false
		}
		return data, true
	}
	data, err := c.GetRawData()
	if err != nil {
		h.writeError(c, err)
		return nil, false
	}
	return data, true
}
