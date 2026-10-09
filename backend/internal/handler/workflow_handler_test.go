package handler

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"project-framework/internal/nodes"
	"project-framework/internal/service/health"
	wfservice "project-framework/internal/service/workflow"
	"project-framework/internal/workflow"
)

func newTestWorkflowService(t *testing.T) *wfservice.Service {
	t.Helper()
	registry := workflow.NewRegistry()
	if err := nodes.Register(registry); err != nil {
		t.Fatal(err)
	}
	catalog, err := workflow.LoadDir("../../workflows", registry)
	if err != nil {
		t.Fatal(err)
	}
	registry.Freeze()
	engine := workflow.NewEngine(context.Background(), workflow.Options{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = engine.Shutdown(ctx)
	})
	return wfservice.New(catalog, registry, engine)
}

func newTestRouter(t *testing.T) http.Handler {
	t.Helper()
	gin.SetMode(gin.TestMode)
	return NewRouter(discardLogger(), healthForTest(), newTestWorkflowService(t))
}

func performBodyRequest(handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func TestWorkflowDiscoveryAndExecution(t *testing.T) {
	router := newTestRouter(t)
	for _, path := range []string{"/readyz", "/api/v1/node-types", "/api/v1/workflows"} {
		if got := performBodyRequest(router, http.MethodGet, path, ""); got.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", path, got.Code, got.Body.String())
		}
	}
	response := performBodyRequest(router, http.MethodGet, "/api/v1/workflows/text-demo/versions/1", "")
	var detail struct {
		Data workflow.View `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if len(detail.Data.Nodes) != 4 || len(detail.Data.Edges) != 4 || len(detail.Data.NodeTypes) != 4 {
		t.Fatalf("invalid compiled view: %+v", detail.Data)
	}
	for _, node := range detail.Data.Nodes {
		if node.TimeoutMS != 30000 || node.Retry.MaxAttempts != 1 {
			t.Fatalf("missing effective policy: %+v", node)
		}
	}
	cases := []struct {
		name, body string
		output     workflow.Object
	}{
		{"text", `{"workflowId":"text-demo","definitionVersion":"1","input":{"text":" Hello "}}`,
			workflow.Object{"upper": "HELLO", "lower": "hello"}},
		{"inconsistent order", `{"workflowId":"order-investigation","definitionVersion":"1","input":{"orderId":"demo-1001"}}`,
			workflow.Object{"consistent": false, "issues": []any{"订单已发货，但配送尚未进入运输状态"}}},
		{"consistent order", `{"workflowId":"order-investigation","definitionVersion":"1","input":{"orderId":"demo-1002"}}`,
			workflow.Object{"consistent": true, "issues": []any{}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			accepted := performBodyRequest(router, http.MethodPost, "/api/v1/runs", tc.body)
			if accepted.Code != http.StatusAccepted {
				t.Fatalf("start: %d %s", accepted.Code, accepted.Body.String())
			}
			location := accepted.Header().Get("Location")
			if !strings.HasPrefix(location, "/api/v1/runs/") {
				t.Fatalf("missing location: %q", location)
			}
			deadline := time.Now().Add(2 * time.Second)
			for {
				got := performBodyRequest(router, http.MethodGet, location, "")
				var result struct {
					Data workflow.Snapshot `json:"data"`
				}
				if err := json.Unmarshal(got.Body.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				snapshot := result.Data
				if snapshot.Status == workflow.StatusSucceeded {
					if !reflect.DeepEqual(snapshot.Output, tc.output) {
						t.Fatalf("output = %#v; want %#v", snapshot.Output, tc.output)
					}
					if !snapshot.Ephemeral || snapshot.InstanceID == "" || len(snapshot.Nodes) != 4 {
						t.Fatalf("invalid snapshot: %+v", snapshot)
					}
					break
				}
				if snapshot.Status != workflow.StatusPending && snapshot.Status != workflow.StatusRunning {
					t.Fatalf("unexpected terminal state: %+v", snapshot)
				}
				if time.Now().After(deadline) {
					t.Fatal("run did not finish")
				}
				time.Sleep(time.Millisecond)
			}
		})
	}
}

func TestWorkflowRequestErrors(t *testing.T) {
	router := newTestRouter(t)
	cases := []struct {
		name, method, path, body string
		status                   int
		code                     string
	}{
		{"missing workflow", "GET", "/api/v1/workflows/missing/versions/1", "", 404, "WORKFLOW_NOT_FOUND"},
		{"missing run", "GET", "/api/v1/runs/missing", "", 404, "RUN_NOT_FOUND"},
		{"malformed", "POST", "/api/v1/runs", "{", 400, "INVALID_REQUEST"},
		{"unknown field", "POST", "/api/v1/runs", `{"unknown":true}`, 400, "INVALID_REQUEST"},
		{"extra JSON", "POST", "/api/v1/runs", `{} {}`, 400, "INVALID_REQUEST"},
		{"missing input", "POST", "/api/v1/runs",
			`{"workflowId":"text-demo","definitionVersion":"1"}`, 400, "INVALID_REQUEST"},
		{"wrong type", "POST", "/api/v1/runs",
			`{"workflowId":"text-demo","definitionVersion":"1","input":{"text":12}}`, 422, "INPUT_INVALID"},
		{"oversized", "POST", "/api/v1/runs",
			`{"workflowId":"text-demo","definitionVersion":"1","input":{"text":"` + strings.Repeat("a", wfservice.MaxRunRequestBytes) + `"}}`,
			413, "PAYLOAD_TOO_LARGE"},
		{"encoded input oversized", "POST", "/api/v1/runs",
			`{"workflowId":"text-demo","definitionVersion":"1","input":{"text":"` + strings.Repeat("<", 50000) + `"}}`,
			413, "PAYLOAD_TOO_LARGE"},
		{"definition invalid", "POST", "/api/v1/workflows/validate", `{"definition":{}}`, 422, "DEFINITION_INVALID"},
		{"draft unknown field", "POST", "/api/v1/test-runs", `{"unknown":true}`, 400, "INVALID_REQUEST"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := performBodyRequest(router, tc.method, tc.path, tc.body)
			var result struct {
				Error errorDetail `json:"error"`
			}
			if err := json.Unmarshal(got.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if got.Code != tc.status || result.Error.Code != tc.code {
				t.Fatalf("got %d %s; want %d %s", got.Code, got.Body.String(), tc.status, tc.code)
			}
		})
	}

	service := newTestWorkflowService(t)
	router = NewRouter(discardLogger(), healthForTest(), service)
	service.SetReady(false)
	for _, check := range []struct{ method, path string }{
		{"GET", "/readyz"},
		{"POST", "/api/v1/runs"},
		{"POST", "/api/v1/test-runs"},
	} {
		if got := performBodyRequest(router, check.method, check.path, "{}"); got.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s shutdown admission: %d %s", check.path, got.Code, got.Body.String())
		}
	}
}

func TestDraftValidateReturnsViewAndYAML(t *testing.T) {
	router := newTestRouter(t)
	view := loadTextView(t)
	encoded, err := json.Marshal(map[string]any{"definition": view.Definition})
	if err != nil {
		t.Fatal(err)
	}
	got := performBodyRequest(router, http.MethodPost, "/api/v1/workflows/validate", string(encoded))
	if got.Code != http.StatusOK {
		t.Fatalf("validate: %d %s", got.Code, got.Body.String())
	}
	var result struct {
		Data struct {
			View workflow.View `json:"view"`
			YAML string        `json:"yaml"`
		} `json:"data"`
	}
	if err := json.Unmarshal(got.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Data.View.Nodes) != 4 || !strings.Contains(result.Data.YAML, "apiVersion: workflow/v1") {
		t.Fatalf("unexpected validate payload: %+v", result.Data)
	}
}

func TestAdmissionSurvivesRequestCancellation(t *testing.T) {
	metadata := workflow.NewRegistry()
	if err := nodes.Register(metadata); err != nil {
		t.Fatal(err)
	}
	registry := workflow.NewRegistry()
	for _, descriptor := range metadata.Descriptors() {
		err := registry.Register(workflow.Registration{
			Descriptor: descriptor,
			Handler: func(ctx context.Context, _ workflow.Call) (workflow.Object, error) {
				<-ctx.Done()
				return nil, ctx.Err()
			},
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	catalog, err := workflow.LoadDir("../../workflows", registry)
	if err != nil {
		t.Fatal(err)
	}
	engine := workflow.NewEngine(context.Background(), workflow.Options{
		MaxActiveRuns: 1, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = engine.Shutdown(ctx)
	})
	service := wfservice.New(catalog, registry, engine)
	router := NewRouter(discardLogger(), healthForTest(), service)

	body := `{"workflowId":"text-demo","definitionVersion":"1","input":{"text":"hello"}}`
	accepted := performBodyRequest(router, http.MethodPost, "/api/v1/runs", body)
	if accepted.Code != http.StatusAccepted {
		t.Fatalf("start: %d %s", accepted.Code, accepted.Body.String())
	}
	got := performBodyRequest(router, http.MethodPost, "/api/v1/runs", body)
	if got.Code != http.StatusTooManyRequests || !strings.Contains(got.Body.String(), "CAPACITY_EXCEEDED") {
		t.Fatalf("admission: %d %s", got.Code, got.Body.String())
	}
	current := performBodyRequest(router, http.MethodGet, accepted.Header().Get("Location"), "")
	var result struct {
		Data workflow.Snapshot `json:"data"`
	}
	if err := json.Unmarshal(current.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Data.Status != workflow.StatusPending && result.Data.Status != workflow.StatusRunning {
		t.Fatalf("service run should continue: %+v", result.Data)
	}
}

func loadTextView(t *testing.T) workflow.View {
	t.Helper()
	service := newTestWorkflowService(t)
	view, err := service.GetWorkflow("text-demo", "1")
	if err != nil {
		t.Fatal(err)
	}
	return view
}

func healthForTest() *health.Service {
	return health.New()
}
