package main

import (
	"context"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/gin-gonic/gin"

	"project-framework/internal/config"
	"project-framework/internal/handler"
	"project-framework/internal/infra/httpserver"
	"project-framework/internal/service/health"
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

	router := handler.NewRouter(logger, health.New())
	server := httpserver.New(cfg.HTTPAddr, cfg.ShutdownTimeout, router, logger)
	logger.Info("http_server_starting", "address", listener.Addr().String())
	if err := server.Run(ctx, listener); err != nil {
		logger.Error("http_server_failed", "error", err)
		os.Exit(1)
	}
}

func configureGinMode() {
	if os.Getenv(gin.EnvGinMode) == "" {
		gin.SetMode(gin.ReleaseMode)
	}
}
