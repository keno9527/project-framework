package main

import (
	"context"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/gin-gonic/gin"

	"project-framework/internal/config"
	"project-framework/internal/handler"
	"project-framework/internal/infra/httpserver"
	"project-framework/internal/nodes"
	"project-framework/internal/service/health"
	wfservice "project-framework/internal/service/workflow"
	"project-framework/internal/workflow"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	configureGinMode()

	cfg, err := config.Load()
	if err != nil {
		logger.Error("load_config_failed", "error", err)
		os.Exit(1)
	}

	listener, err := net.Listen("tcp", cfg.HTTPAddr)
	if err != nil {
		logger.Error("http_listener_failed", "address", cfg.HTTPAddr, "error", err)
		os.Exit(1)
	}
	defer listener.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	workflowService, err := buildWorkflowService(ctx, cfg, logger)
	if err != nil {
		logger.Error("workflow_bootstrap_failed", "error", err)
		os.Exit(1)
	}

	router := handler.NewRouter(logger, health.New(), workflowService)
	server := httpserver.New(cfg.HTTPAddr, cfg.ShutdownTimeout, router, logger)
	logger.Info("http_server_starting", "address", listener.Addr().String(),
		"workflow_versions", len(workflowService.Workflows()))
	serveErr := server.Run(ctx, listener)

	workflowService.SetReady(false)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := workflowService.Shutdown(shutdownCtx); err != nil {
		logger.Error("workflow_engine_shutdown_failed", "error", err)
	}

	if serveErr != nil {
		logger.Error("http_server_failed", "error", serveErr)
		os.Exit(1)
	}
}

func buildWorkflowService(ctx context.Context, cfg config.Config, logger *slog.Logger) (*wfservice.Service, error) {
	registry := workflow.NewRegistry()
	if err := nodes.Register(registry); err != nil {
		return nil, err
	}
	absDir, err := filepath.Abs(cfg.WorkflowDir)
	if err != nil {
		return nil, err
	}
	catalog, err := workflow.LoadDir(absDir, registry)
	if err != nil {
		return nil, err
	}
	registry.Freeze()
	engine := workflow.NewEngine(ctx, workflow.Options{
		MaxActiveRuns:      cfg.MaxActiveRuns,
		MaxConcurrentNodes: cfg.MaxConcurrentNodes,
		MaxNodesPerRun:     cfg.MaxNodesPerRun,
		RunTimeout:         cfg.RunTimeout,
		Logger:             logger,
	})
	return wfservice.New(catalog, registry, engine), nil
}

func configureGinMode() {
	if os.Getenv(gin.EnvGinMode) == "" {
		gin.SetMode(gin.ReleaseMode)
	}
}
