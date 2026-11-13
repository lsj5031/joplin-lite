package assistant

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestConnectionUsesConfiguredEndpointAndCredential(t *testing.T) {
	var gotPath, gotAuth, gotModel string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotModel, _ = body["model"].(string)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"OK"}}]}`))
	}))
	defer server.Close()
	m := &Manager{settings: DefaultSettings()}
	settings := DefaultSettings()
	settings.BaseURL = server.URL + "/v1"
	settings.ModelID = "test-model"
	settings.APIKey = "session-key"
	if err := m.TestConnection(context.Background(), settings); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/v1/chat/completions" {
		t.Fatalf("test hit %q", gotPath)
	}
	if gotAuth != "Bearer session-key" {
		t.Fatalf("test sent %q", gotAuth)
	}
	if gotModel != "test-model" {
		t.Fatalf("test asked for model %q", gotModel)
	}
}

func TestConnectionAllowsKeylessLocalEndpoint(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"OK"}}]}`))
	}))
	defer server.Close()
	settings := DefaultSettings()
	settings.BaseURL = server.URL + "/v1"
	settings.ModelID = "local-model"
	if !settings.Configured() {
		t.Fatal("a keyless local endpoint should be configured")
	}
	m := &Manager{settings: DefaultSettings()}
	if err := m.TestConnection(context.Background(), settings); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "" {
		t.Fatalf("keyless endpoint received Authorization %q", gotAuth)
	}
}

func TestConnectionReportsProviderFailureWithoutLeakingTheKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid token"}}`))
	}))
	defer server.Close()
	m := &Manager{settings: DefaultSettings()}
	settings := DefaultSettings()
	settings.BaseURL = server.URL + "/v1"
	settings.ModelID = "test-model"
	settings.APIKey = "super-secret-key"
	err := m.TestConnection(context.Background(), settings)
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("expected an HTTP 401 error, got %v", err)
	}
	if strings.Contains(err.Error(), settings.APIKey) {
		t.Fatal("connection error leaked the API key")
	}
}
