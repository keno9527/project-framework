package handler

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"project-framework/internal/service/health"
)

func NewRouter(logger *slog.Logger, healthService *health.Service) http.Handler {
	router := gin.New()
	router.HandleMethodNotAllowed = true
	router.Use(
		requestIDMiddleware(),
		accessLogMiddleware(logger),
		recoveryMiddleware(logger),
	)

	router.GET("/healthz", healthHandler(healthService))
	router.NoRoute(func(c *gin.Context) {
		writeAPIError(c, http.StatusNotFound, "not_found", "resource not found")
	})
	router.NoMethod(func(c *gin.Context) {
		writeAPIError(c, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	})

	return router
}
