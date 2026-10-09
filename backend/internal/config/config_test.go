package config

import (
	"testing"
	"time"
)

func TestLoadUsesDefaults(t *testing.T) {
	t.Setenv("HTTP_ADDR", "")
	t.Setenv("SHUTDOWN_TIMEOUT", "")
	t.Setenv("WORKFLOW_DIR", "")
	t.Setenv("MAX_ACTIVE_RUNS", "")
	t.Setenv("MAX_CONCURRENT_NODES", "")
	t.Setenv("MAX_NODES_PER_RUN", "")
	t.Setenv("RUN_TIMEOUT", "")

	got, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if got.HTTPAddr != ":8080" {
		t.Fatalf("HTTPAddr = %q, want %q", got.HTTPAddr, ":8080")
	}
	if got.ShutdownTimeout != 10*time.Second {
		t.Fatalf("ShutdownTimeout = %s, want %s", got.ShutdownTimeout, 10*time.Second)
	}
	if got.WorkflowDir != "./workflows" {
		t.Fatalf("WorkflowDir = %q, want %q", got.WorkflowDir, "./workflows")
	}
	if got.MaxActiveRuns != 8 {
		t.Fatalf("MaxActiveRuns = %d, want 8", got.MaxActiveRuns)
	}
	if got.MaxConcurrentNodes != 16 {
		t.Fatalf("MaxConcurrentNodes = %d, want 16", got.MaxConcurrentNodes)
	}
	if got.MaxNodesPerRun != 4 {
		t.Fatalf("MaxNodesPerRun = %d, want 4", got.MaxNodesPerRun)
	}
	if got.RunTimeout != 2*time.Minute {
		t.Fatalf("RunTimeout = %s, want %s", got.RunTimeout, 2*time.Minute)
	}
}

func TestLoadReadsWorkflowEnvironment(t *testing.T) {
	t.Setenv("WORKFLOW_DIR", "/tmp/workflows")
	t.Setenv("MAX_ACTIVE_RUNS", "12")
	t.Setenv("MAX_CONCURRENT_NODES", "20")
	t.Setenv("MAX_NODES_PER_RUN", "6")
	t.Setenv("RUN_TIMEOUT", "90s")

	got, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if got.WorkflowDir != "/tmp/workflows" {
		t.Fatalf("WorkflowDir = %q, want %q", got.WorkflowDir, "/tmp/workflows")
	}
	if got.MaxActiveRuns != 12 {
		t.Fatalf("MaxActiveRuns = %d, want 12", got.MaxActiveRuns)
	}
	if got.MaxConcurrentNodes != 20 {
		t.Fatalf("MaxConcurrentNodes = %d, want 20", got.MaxConcurrentNodes)
	}
	if got.MaxNodesPerRun != 6 {
		t.Fatalf("MaxNodesPerRun = %d, want 6", got.MaxNodesPerRun)
	}
	if got.RunTimeout != 90*time.Second {
		t.Fatalf("RunTimeout = %s, want %s", got.RunTimeout, 90*time.Second)
	}
}

func TestLoadRejectsInvalidRunTimeout(t *testing.T) {
	t.Setenv("RUN_TIMEOUT", "soon")

	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want invalid timeout error")
	}
}

func TestLoadRejectsNonPositiveRunTimeout(t *testing.T) {
	t.Setenv("RUN_TIMEOUT", "0s")

	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want non-positive timeout error")
	}
}

func TestLoadRejectsInvalidLimit(t *testing.T) {
	for _, key := range []string{"MAX_ACTIVE_RUNS", "MAX_CONCURRENT_NODES", "MAX_NODES_PER_RUN"} {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, "many")
			if _, err := Load(); err == nil {
				t.Fatalf("Load() error = nil, want invalid %s error", key)
			}
		})
	}
}

func TestLoadRejectsNonPositiveLimit(t *testing.T) {
	for _, key := range []string{"MAX_ACTIVE_RUNS", "MAX_CONCURRENT_NODES", "MAX_NODES_PER_RUN"} {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, "0")
			if _, err := Load(); err == nil {
				t.Fatalf("Load() error = nil, want non-positive %s error", key)
			}
		})
	}
}

func TestLoadReadsEnvironment(t *testing.T) {
	t.Setenv("HTTP_ADDR", "127.0.0.1:9000")
	t.Setenv("SHUTDOWN_TIMEOUT", "3s")

	got, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if got.HTTPAddr != "127.0.0.1:9000" {
		t.Fatalf("HTTPAddr = %q, want %q", got.HTTPAddr, "127.0.0.1:9000")
	}
	if got.ShutdownTimeout != 3*time.Second {
		t.Fatalf("ShutdownTimeout = %s, want %s", got.ShutdownTimeout, 3*time.Second)
	}
}

func TestLoadRejectsInvalidShutdownTimeout(t *testing.T) {
	t.Setenv("SHUTDOWN_TIMEOUT", "later")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want invalid timeout error")
	}
}

func TestLoadRejectsNonPositiveShutdownTimeout(t *testing.T) {
	t.Setenv("SHUTDOWN_TIMEOUT", "0s")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() error = nil, want non-positive timeout error")
	}
}
