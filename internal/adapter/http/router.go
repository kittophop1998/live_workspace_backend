package http

import (
	"net/http"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"

	"kingdom_manager/backend/internal/adapter/http/handler"
	"kingdom_manager/backend/internal/adapter/http/middleware"
	"kingdom_manager/backend/internal/adapter/http/realtime"
)

func NewRouter(h *handler.Handler, auth *middleware.Auth, hub *realtime.Hub, origins []string) *gin.Engine {
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
		v1.POST("/resources/:id/fields", h.AddField)
		v1.PATCH("/resources/:id/fields/:field_id", h.UpdateField)
		v1.DELETE("/resources/:id/fields/:field_id", h.DeleteField)
		v1.GET("/resources/:id/comments", h.Comments)
		v1.POST("/resources/:id/comments", h.AddComment)
		v1.DELETE("/comments/:id", h.DeleteComment)
		v1.GET("/activity", h.Activity)

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

		v1.GET("/stream", hub.Serve)
	}
	return router
}
