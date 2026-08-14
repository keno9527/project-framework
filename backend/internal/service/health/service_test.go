package health

import (
	"context"
	"testing"

	"project-framework/internal/model"
)

func TestServiceCheckReturnsOK(t *testing.T) {
	report := New().Check(context.Background())

	if report.Status != model.HealthStatusOK {
		t.Fatalf("Status = %q, want %q", report.Status, model.HealthStatusOK)
	}
}
