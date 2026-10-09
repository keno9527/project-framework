package handler

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"project-framework/internal/service/health"
	wfservice "project-framework/internal/service/workflow"
)

func NewRouter(logger *slog.Logger, healthService *health.Service, workflowService *wfservice.Service) http.Handler {
	router := gin.New()
	router.HandleMethodNotAllowed = true
	router.Use(
		requestIDMiddleware(),
		accessLogMiddleware(logger),
		recoveryMiddleware(logger),
	)

	router.GET("/healthz", healthHandler(healthService))
	registerWorkflowRoutes(router, workflowService)

	router.NoRoute(func(c *gin.Context) {
		writeAPIError(c, http.StatusNotFound, "not_found", "resource not found")
	})
	router.NoMethod(func(c *gin.Context) {
		writeAPIError(c, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	})

	return router
}
