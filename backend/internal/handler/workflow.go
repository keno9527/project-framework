package handler

import (
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	wfservice "project-framework/internal/service/workflow"
	"project-framework/internal/workflow"
)

type workflowHandlers struct {
	service *wfservice.Service
}

func registerWorkflowRoutes(router *gin.Engine, service *wfservice.Service) {
	h := &workflowHandlers{service: service}

	router.GET("/readyz", h.ready)

	v1 := router.Group("/api/v1")
	v1.GET("/node-types", h.listNodeTypes)
	v1.GET("/workflows", h.listWorkflows)
	v1.POST("/workflows/validate", h.validateWorkflow)
	v1.GET("/workflows/:workflowId/versions/:version", h.getWorkflow)
	v1.POST("/test-runs", h.startTestRun)
	v1.POST("/runs", h.startRun)
	v1.GET("/runs/:runId", h.getRun)
}

func (h *workflowHandlers) ready(c *gin.Context) {
	if !h.service.Ready() {
		writeWorkflowError(c, http.StatusServiceUnavailable, "NOT_READY", "service is shutting down")
		return
	}
	writeData(c, http.StatusOK, gin.H{"status": "ready", "instanceId": h.service.InstanceID()})
}

func (h *workflowHandlers) listNodeTypes(c *gin.Context) {
	writeData(c, http.StatusOK, gin.H{"items": h.service.NodeTypes()})
}

func (h *workflowHandlers) listWorkflows(c *gin.Context) {
	writeData(c, http.StatusOK, gin.H{"items": h.service.Workflows()})
}

func (h *workflowHandlers) getWorkflow(c *gin.Context) {
	view, err := h.service.GetWorkflow(c.Param("workflowId"), c.Param("version"))
	if err != nil {
		writeFailure(c, err)
		return
	}
	writeData(c, http.StatusOK, view)
}

func (h *workflowHandlers) getRun(c *gin.Context) {
	snapshot, err := h.service.GetRun(c.Param("runId"))
	if err != nil {
		writeFailure(c, err)
		return
	}
	writeData(c, http.StatusOK, snapshot)
}

func (h *workflowHandlers) validateWorkflow(c *gin.Context) {
	body, ok := readLimitedBody(c, wfservice.MaxDraftRequestBytes)
	if !ok {
		return
	}
	view, exported, err := h.service.ValidateDraft(body)
	if err != nil {
		writeFailure(c, err)
		return
	}
	writeData(c, http.StatusOK, gin.H{"view": view, "yaml": exported})
}

func (h *workflowHandlers) startTestRun(c *gin.Context) {
	body, ok := readLimitedBody(c, wfservice.MaxDraftRequestBytes)
	if !ok {
		return
	}
	snapshot, err := h.service.StartTestRun(body)
	if err != nil {
		writeFailure(c, err)
		return
	}
	writeAcceptedRun(c, snapshot)
}

func (h *workflowHandlers) startRun(c *gin.Context) {
	body, ok := readLimitedBody(c, wfservice.MaxRunRequestBytes)
	if !ok {
		return
	}
	snapshot, err := h.service.StartRun(body)
	if err != nil {
		writeFailure(c, err)
		return
	}
	writeAcceptedRun(c, snapshot)
}

func readLimitedBody(c *gin.Context, limit int64) ([]byte, bool) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		var sizeError *http.MaxBytesError
		if errors.As(err, &sizeError) {
			writeWorkflowError(c, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", requestTooLargeMessage(limit))
		} else {
			writeWorkflowError(c, http.StatusBadRequest, "INVALID_REQUEST", "could not read request")
		}
		return nil, false
	}
	return body, true
}

func requestTooLargeMessage(limit int64) string {
	switch limit {
	case wfservice.MaxRunRequestBytes:
		return "request exceeds 256 KiB"
	default:
		return "request exceeds 2 MiB"
	}
}

func writeAcceptedRun(c *gin.Context, snapshot workflow.Snapshot) {
	c.Header("Location", "/api/v1/runs/"+snapshot.RunID)
	writeData(c, http.StatusAccepted, snapshot)
}

func writeData(c *gin.Context, status int, data any) {
	c.JSON(status, gin.H{"data": data})
}

func writeFailure(c *gin.Context, err error) {
	var failure *wfservice.Failure
	if errors.As(err, &failure) {
		writeWorkflowError(c, failureStatus(failure.Kind), string(failure.Kind), failure.Message)
		return
	}
	writeWorkflowError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
}

func failureStatus(kind wfservice.Kind) int {
	switch kind {
	case wfservice.KindNotReady:
		return http.StatusServiceUnavailable
	case wfservice.KindInvalidRequest:
		return http.StatusBadRequest
	case wfservice.KindDefinitionInvalid, wfservice.KindInputInvalid:
		return http.StatusUnprocessableEntity
	case wfservice.KindWorkflowNotFound, wfservice.KindRunNotFound:
		return http.StatusNotFound
	case wfservice.KindPayloadTooLarge:
		return http.StatusRequestEntityTooLarge
	case wfservice.KindCapacityExceeded:
		return http.StatusTooManyRequests
	default:
		return http.StatusInternalServerError
	}
}

func writeWorkflowError(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, errorResponse{
		Error:     errorDetail{Code: code, Message: message},
		RequestID: requestIDFromContext(c),
	})
}
