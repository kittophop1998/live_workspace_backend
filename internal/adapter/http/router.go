package http

import (
	"net/http"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"

	"kingdom_manager/backend/internal/adapter/http/handler"
	"kingdom_manager/backend/internal/adapter/http/middleware"
	"kingdom_manager/backend/internal/adapter/http/realtime"
)

func NewRouter(h *handler.Handler, apiSpec *handler.APISpecHandler, apiKeys *handler.APIKeyHandler, auth *middleware.Auth, keyAuth *middleware.APIKeyAuth, hub *realtime.Hub, origins []string) *gin.Engine {
	router := gin.New()
	router.Use(gin.Logger(), gin.Recovery(), cors.New(cors.Config{
		AllowOrigins: origins, AllowMethods: []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders: []string{"Authorization", "Content-Type", "If-Match", "X-Confirm-Delete-All"},
	}))
	router.GET("/health", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	router.POST("/api/v1/rooms", h.CreateRoom)
	router.POST("/api/v1/rooms/join", h.JoinRoom)
	v1 := router.Group("/api/v1", auth.Handler())
	{
		v1.POST("/api-keys", apiKeys.Create)
		v1.GET("/api-keys", apiKeys.List)
		v1.DELETE("/api-keys/:id", apiKeys.Revoke)
		v1.GET("/workspace", h.Workspace)
		v1.GET("/workspace/collaborators", h.Collaborators)
		v1.GET("/me", h.Me)
		v1.GET("/resources", h.ListResources)
		v1.GET("/resources/:id", h.GetResource)
		v1.POST("/resources", h.CreateResource)
		v1.POST("/resources/import", h.ImportResources)
		v1.DELETE("/resources", h.DeleteAllResources)
		v1.PATCH("/resources/:id", h.UpdateResource)
		v1.DELETE("/resources/:id", h.DeleteResource)
		v1.PUT("/resources/:id/responses", h.ReplaceResponses)
		v1.PUT("/resources/:id/request-fields", h.ReplaceRequestFields)
		v1.POST("/resources/:id/fields", h.AddField)
		v1.PATCH("/resources/:id/fields/:field_id", h.UpdateField)
		v1.DELETE("/resources/:id/fields/:field_id", h.DeleteField)
		v1.GET("/resources/:id/comments", h.Comments)
		v1.POST("/resources/:id/comments", h.AddComment)
		v1.DELETE("/comments/:id", h.DeleteComment)
		v1.GET("/activity", h.Activity)

		// Project-wide team chat (append-only; broadcast as `chat.created`).
		v1.GET("/chat", h.ChatMessages)
		v1.POST("/chat", h.SendChatMessage)

		// Backend work-update log (append-only; broadcast as `task_log.created`,
		// like toggles as `task_log.updated`).
		v1.GET("/task-logs", h.TaskLogs)
		v1.POST("/task-logs", h.AddTaskLog)
		v1.POST("/task-logs/:id/like", h.ToggleTaskLogLike)

		// API testing (single-request proxy) + E2E flow testing.
		v1.POST("/http/test", h.HTTPTest)
		v1.POST("/flows/parse", h.ParseFlow)
		v1.POST("/flows", h.SaveFlow)
		v1.GET("/flows", h.ListFlows)
		v1.GET("/flows/runs/:run_id", h.GetFlowRun)
		v1.GET("/flows/:id", h.GetFlow)
		v1.DELETE("/flows/:id", h.DeleteFlow)
		v1.POST("/flows/:id/run", h.RunFlow)
		v1.GET("/flows/:id/runs", h.ListFlowRuns)

		v1.POST("/stories", h.CreateStory)
		v1.GET("/stories", h.ListStories)
		v1.GET("/stories/:id", h.GetStory)
		v1.PATCH("/stories/:id", h.UpdateStory)
		v1.DELETE("/stories/:id", h.DeleteStory)

		v1.POST("/proposals", h.CreateProposal)
		v1.GET("/proposals", h.ListProposals)
		v1.GET("/proposals/:id", h.GetProposal)
		v1.PATCH("/proposals/:id", h.UpdateProposal)
		v1.DELETE("/proposals/:id", h.DeleteProposal)
		v1.POST("/proposals/:id/status", h.SetProposalStatus)
		v1.POST("/proposals/:id/fields", h.AddProposalField)
		v1.PATCH("/proposals/:id/fields/:field_id", h.UpdateProposalField)
		v1.DELETE("/proposals/:id/fields/:field_id", h.RemoveProposalField)
		v1.POST("/proposals/:id/comments", h.AddProposalComment)
		v1.PATCH("/proposals/:id/comments/:comment_id", h.ResolveProposalComment)

		// Usage feedback — complaints / improvement requests with a status lifecycle.
		v1.POST("/feedback", h.CreateFeedback)
		v1.GET("/feedback", h.ListFeedback)
		v1.POST("/feedback/:id/status", h.SetFeedbackStatus)
		v1.DELETE("/feedback/:id", h.DeleteFeedback)

		v1.GET("/stream", hub.Serve)
	}
	cli := router.Group("/api/v1")
	cli.GET("/cli/me", keyAuth.Require("api-spec:read"), apiSpec.Me)
	cli.POST("/projects/:projectId/api-spec/revisions", keyAuth.Require("api-spec:write"), apiSpec.Publish)
	cli.GET("/projects/:projectId/api-spec", keyAuth.Require("api-spec:read"), apiSpec.Current)
	cli.GET("/projects/:projectId/api-spec/revisions", keyAuth.Require("api-spec:revision:read"), apiSpec.List)
	cli.GET("/projects/:projectId/api-spec/revisions/:revisionId", keyAuth.Require("api-spec:read"), apiSpec.Get)
	return router
}
