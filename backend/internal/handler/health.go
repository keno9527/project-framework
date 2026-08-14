package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"project-framework/internal/model"
	"project-framework/internal/service/health"
)

type healthResponse struct {
	Status model.HealthStatus `json:"status"`
}

func healthHandler(healthService *health.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		report := healthService.Check(c.Request.Context())
		c.JSON(http.StatusOK, healthResponse{Status: report.Status})
	}
}
