package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
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

func main() {
	if err := run(); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	rootCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	connectCtx, cancel := context.WithTimeout(rootCtx, 10*time.Second)
	defer cancel()
	client, err := mongo.Connect(connectCtx, options.Client().ApplyURI(cfg.MongoURI))
	if err != nil {
		return err
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := client.Disconnect(ctx); err != nil {
			slog.Error("disconnect mongo", "error", err)
		}
	}()
	if err := client.Ping(connectCtx, nil); err != nil {
		return err
	}

	repository := mongorepo.NewWorkspaceRepository(client.Database(cfg.MongoDatabase))
	if err := repository.EnsureIndexes(connectCtx); err != nil {
		return err
	}
	flowRepository := mongorepo.NewFlowRepository(client.Database(cfg.MongoDatabase))
	if err := flowRepository.EnsureIndexes(connectCtx); err != nil {
		return err
	}
	hub := realtime.NewHub(cfg.AllowedOrigins)
	service := usecase.NewService(repository, cfg.WorkspaceID, hub)
	roomService := usecase.NewRoomService(repository)
	// Dev tool: allow proxying to private/localhost hosts so devs can test local APIs.
	executor := httpexec.New(true)
	flowService := usecase.NewFlowService(flowRepository, service, arazzo.Parser{}, executor)
	hub.SetService(service)
	auth := middleware.NewAuth(cfg.JWTSecret, 30*24*time.Hour)
	apiHandler := handler.New(service, roomService, flowService, executor, auth)
	router := httpadapter.NewRouter(apiHandler, auth, hub, cfg.AllowedOrigins)
	mcpServer := mcpadapter.NewServer(service, flowService, slog.Default())
	mcpadapter.Mount(router, cfg.MCPEnabled, cfg.MCPPath, auth, mcpServer)
	if cfg.MCPEnabled {
		slog.Info("MCP enabled", "path", cfg.MCPPath)
	}
	server := &http.Server{Addr: cfg.HTTPAddr, Handler: router, ReadHeaderTimeout: 5 * time.Second}

	go hub.Run(rootCtx)
	errs := make(chan error, 1)
	go func() {
		slog.Info("API listening", "address", cfg.HTTPAddr)
		errs <- server.ListenAndServe()
	}()

	select {
	case <-rootCtx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	case err := <-errs:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
