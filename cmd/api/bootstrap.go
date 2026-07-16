package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	httpadapter "kingdom_manager/backend/internal/adapter/http"
	"kingdom_manager/backend/internal/adapter/http/handler"
	"kingdom_manager/backend/internal/adapter/http/middleware"
	"kingdom_manager/backend/internal/adapter/http/realtime"
	mcpadapter "kingdom_manager/backend/internal/adapter/mcp"
	mongorepo "kingdom_manager/backend/internal/adapter/repository/mongo"
	"kingdom_manager/backend/internal/arazzo"
	"kingdom_manager/backend/internal/config"
	"kingdom_manager/backend/internal/httpexec"
	"kingdom_manager/backend/internal/usecase"
)

const (
	mongoConnectTimeout = 10 * time.Second
	mongoCloseTimeout   = 5 * time.Second
	serverCloseTimeout  = 10 * time.Second
	readHeaderTimeout   = 5 * time.Second
	authTokenTTL        = 30 * 24 * time.Hour
)

type application struct {
	server *http.Server
	hub    *realtime.Hub
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	rootCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	client, err := connectMongo(rootCtx, cfg.MongoURI)
	if err != nil {
		return err
	}
	defer disconnectMongo(client)

	app, err := buildApplication(rootCtx, cfg, client)
	if err != nil {
		return err
	}
	return serve(rootCtx, app)
}

func connectMongo(ctx context.Context, uri string) (*mongo.Client, error) {
	connectCtx, cancel := context.WithTimeout(ctx, mongoConnectTimeout)
	defer cancel()

	client, err := mongo.Connect(connectCtx, options.Client().ApplyURI(uri))
	if err != nil {
		return nil, err
	}
	if err := client.Ping(connectCtx, nil); err != nil {
		disconnectMongo(client)
		return nil, err
	}
	return client, nil
}

func disconnectMongo(client *mongo.Client) {
	ctx, cancel := context.WithTimeout(context.Background(), mongoCloseTimeout)
	defer cancel()
	if err := client.Disconnect(ctx); err != nil {
		slog.Error("disconnect mongo", "error", err)
	}
}

func buildApplication(ctx context.Context, cfg config.Config, client *mongo.Client) (*application, error) {
	database := client.Database(cfg.MongoDatabase)

	workspaceRepository := mongorepo.NewWorkspaceRepository(database)
	if err := workspaceRepository.EnsureIndexes(ctx); err != nil {
		return nil, err
	}
	if err := workspaceRepository.MigrateLegacy(ctx); err != nil {
		return nil, err
	}

	flowRepository := mongorepo.NewFlowRepository(database)
	if err := flowRepository.EnsureIndexes(ctx); err != nil {
		return nil, err
	}

	storyRepository := mongorepo.NewStoryRepository(database)
	if err := storyRepository.EnsureIndexes(ctx); err != nil {
		return nil, err
	}

	proposalRepository := mongorepo.NewProposalRepository(database)
	if err := proposalRepository.EnsureIndexes(ctx); err != nil {
		return nil, err
	}

	feedbackRepository := mongorepo.NewFeedbackRepository(database)
	if err := feedbackRepository.EnsureIndexes(ctx); err != nil {
		return nil, err
	}

	chatRepository := mongorepo.NewChatRepository(database)
	if err := chatRepository.EnsureIndexes(ctx); err != nil {
		return nil, err
	}

	taskLogRepository := mongorepo.NewTaskLogRepository(database)
	if err := taskLogRepository.EnsureIndexes(ctx); err != nil {
		return nil, err
	}
	apiSpecRepository := mongorepo.NewAPISpecRepository(database, client)
	if err := apiSpecRepository.EnsureIndexes(ctx); err != nil {
		return nil, err
	}
	apiKeyRepository := mongorepo.NewAPIKeyRepository(database)
	if err := apiKeyRepository.EnsureIndexes(ctx); err != nil {
		return nil, err
	}

	hub := realtime.NewHub(cfg.AllowedOrigins)
	workspaceService := usecase.NewService(workspaceRepository, chatRepository, taskLogRepository, cfg.WorkspaceID, hub)
	roomService := usecase.NewRoomService(workspaceRepository)
	storyService := usecase.NewStoryService(storyRepository)
	proposalService := usecase.NewProposalService(proposalRepository)
	feedbackService := usecase.NewFeedbackService(feedbackRepository)
	apiSpecService := usecase.NewAPISpecService(apiSpecRepository, hub)
	apiKeyService := usecase.NewAPIKeyService(apiKeyRepository)

	// Dev tool: allow proxying to private/localhost hosts so devs can test local APIs.
	executor := httpexec.New(true)
	flowService := usecase.NewFlowService(flowRepository, workspaceService, arazzo.Parser{}, executor)
	hub.SetService(workspaceService)

	auth := middleware.NewAuth(cfg.JWTSecret, authTokenTTL)
	apiHandler := handler.New(workspaceService, roomService, flowService, storyService, proposalService, feedbackService, executor, auth)
	router := httpadapter.NewRouter(apiHandler, handler.NewAPISpecHandler(apiSpecService), handler.NewAPIKeyHandler(apiKeyService), auth, middleware.NewAPIKeyAuth(apiKeyService), hub, cfg.AllowedOrigins)

	mcpServer := mcpadapter.NewServer(workspaceService, flowService, slog.Default())
	mcpadapter.Mount(router, cfg.MCPEnabled, cfg.MCPPath, auth, mcpServer)
	if cfg.MCPEnabled {
		slog.Info("MCP enabled", "path", cfg.MCPPath)
	}

	return &application{
		hub: hub,
		server: &http.Server{
			Addr:              cfg.HTTPAddr,
			Handler:           router,
			ReadHeaderTimeout: readHeaderTimeout,
		},
	}, nil
}

func serve(ctx context.Context, app *application) error {
	go app.hub.Run(ctx)

	errs := make(chan error, 1)
	go func() {
		slog.Info("API listening", "address", app.server.Addr)
		errs <- app.server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), serverCloseTimeout)
		defer cancel()
		return app.server.Shutdown(shutdownCtx)
	case err := <-errs:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
