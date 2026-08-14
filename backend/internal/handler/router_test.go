package handler

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"project-framework/internal/model"
	"project-framework/internal/service/health"
)

func TestHealthEndpointReturnsServiceStatus(t *testing.T) {
	router := NewRouter(discardLogger(), health.New())
	response := performRequest(router, http.MethodGet, "/healthz", "")

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if got := response.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}

	var body healthResponse
	decodeJSON(t, response, &body)
	if body.Status != model.HealthStatusOK {
		t.Fatalf("status body = %q, want %q", body.Status, model.HealthStatusOK)
	}
}

func TestRouterReturnsStableNotFoundError(t *testing.T) {
	router := NewRouter(discardLogger(), health.New())
	response := performRequest(router, http.MethodGet, "/missing", "request-123")

	assertAPIError(t, response, http.StatusNotFound, "not_found", "resource not found", "request-123")
}

func TestRouterReturnsStableMethodNotAllowedError(t *testing.T) {
	router := NewRouter(discardLogger(), health.New())
	response := performRequest(router, http.MethodPost, "/healthz", "request-123")

	assertAPIError(t, response, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed", "request-123")
}

func TestRequestIDMiddlewarePreservesIncomingID(t *testing.T) {
	router := NewRouter(discardLogger(), health.New())
	response := performRequest(router, http.MethodGet, "/healthz", "request-123")

	if got := response.Header().Get(requestIDHeader); got != "request-123" {
		t.Fatalf("%s = %q, want %q", requestIDHeader, got, "request-123")
	}
}

func TestRequestIDMiddlewareGeneratesMissingID(t *testing.T) {
	router := NewRouter(discardLogger(), health.New())
	response := performRequest(router, http.MethodGet, "/healthz", "")

	if got := response.Header().Get(requestIDHeader); got == "" {
		t.Fatalf("%s is empty", requestIDHeader)
	}
}

func TestRecoveryMiddlewareMapsPanicToInternalError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(requestIDMiddleware(), recoveryMiddleware(discardLogger()))
	router.GET("/panic", func(*gin.Context) {
		panic("boom")
	})

	response := performRequest(router, http.MethodGet, "/panic", "request-123")

	assertAPIError(t, response, http.StatusInternalServerError, "internal_error", "internal server error", "request-123")
}

func TestAccessLogContainsRequestContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	router := gin.New()
	router.Use(requestIDMiddleware(), accessLogMiddleware(logger))
	router.GET("/healthz", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	response := performRequest(router, http.MethodGet, "/healthz", "request-123")
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
	}

	logLine := output.String()
	for _, want := range []string{
		`"msg":"http_request"`,
		`"request_id":"request-123"`,
		`"method":"GET"`,
		`"path":"/healthz"`,
		`"status":204`,
		`"duration_ms":`,
	} {
		if !strings.Contains(logLine, want) {
			t.Fatalf("log = %q, want field %q", logLine, want)
		}
	}
}

func performRequest(handler http.Handler, method, path, requestID string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, nil)
	if requestID != "" {
		request.Header.Set(requestIDHeader, requestID)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func assertAPIError(t *testing.T, response *httptest.ResponseRecorder, status int, code, message, requestID string) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("status = %d, want %d", response.Code, status)
	}

	var body errorResponse
	decodeJSON(t, response, &body)
	if body.Error.Code != code {
		t.Fatalf("error code = %q, want %q", body.Error.Code, code)
	}
	if body.Error.Message != message {
		t.Fatalf("error message = %q, want %q", body.Error.Message, message)
	}
	if body.RequestID != requestID {
		t.Fatalf("request ID = %q, want %q", body.RequestID, requestID)
	}
}

func decodeJSON(t *testing.T, response *httptest.ResponseRecorder, target any) {
	t.Helper()
	if err := json.Unmarshal(response.Body.Bytes(), target); err != nil {
		t.Fatalf("decode response %q: %v", response.Body.String(), err)
	}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
}
