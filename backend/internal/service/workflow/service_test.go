package workflow

import (
	"encoding/json"
	"strings"
	"testing"

	"project-framework/internal/workflow"
)

func TestRedactNestedCredentials(t *testing.T) {
	value := workflow.Object{
		"password": "secret",
		"nested":   map[string]any{"api_key": "secret", "name": "visible"},
		"array":    []any{map[string]any{"authorization": "secret"}},
	}
	encoded, err := json.Marshal(redact(value))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "secret") || !strings.Contains(string(encoded), "visible") {
		t.Fatalf("unexpected redaction: %s", encoded)
	}
}

func TestSensitiveKeyNormalizesSeparators(t *testing.T) {
	for _, key := range []string{"client_secret", "ClientSecret", "ACCESS-TOKEN", "ApiKey"} {
		if !sensitiveKey(key) {
			t.Fatalf("sensitiveKey(%q) = false, want true", key)
		}
	}
	if sensitiveKey("username") {
		t.Fatal("sensitiveKey(username) = true, want false")
	}
}
