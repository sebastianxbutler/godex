package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"godex/pkg/config"
	"godex/pkg/protocol"
)

func TestNewClient(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			name: "valid config",
			cfg: Config{
				Name:    "test",
				BaseURL: "http://localhost:8080/v1",
			},
			wantErr: false,
		},
		{
			name: "missing base_url",
			cfg: Config{
				Name: "test",
			},
			wantErr: true,
		},
		{
			name: "with api key",
			cfg: Config{
				Name:    "test",
				BaseURL: "http://localhost:8080/v1",
				Auth: config.BackendAuthConfig{
					Type: "api_key",
					Key:  "test-key",
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("New() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestClientName(t *testing.T) {
	c, _ := New(Config{Name: "ollama", BaseURL: "http://localhost:11434/v1"})
	if c.Name() != "ollama" {
		t.Errorf("Name() = %s, want ollama", c.Name())
	}
}

func TestClientListModelsHardcoded(t *testing.T) {
	c, _ := New(Config{
		Name:      "test",
		BaseURL:   "http://localhost:8080/v1",
		Discovery: false,
		Models: []config.BackendModelDef{
			{ID: "model-a", DisplayName: "Model A"},
			{ID: "model-b", DisplayName: "Model B"},
		},
	})

	models, err := c.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if len(models) != 2 {
		t.Errorf("expected 2 models, got %d", len(models))
	}
	if models[0].ID != "model-a" {
		t.Errorf("expected model-a, got %s", models[0].ID)
	}
}

func TestClientListModelsDiscovery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"id": "discovered-model-1"},
				{"id": "discovered-model-2"},
			},
		})
	}))
	defer server.Close()

	c, _ := New(Config{
		Name:      "test",
		BaseURL:   server.URL + "/v1",
		Discovery: true,
	})

	models, err := c.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if len(models) != 2 {
		t.Errorf("expected 2 models, got %d", len(models))
	}
	if models[0].ID != "discovered-model-1" {
		t.Errorf("expected discovered-model-1, got %s", models[0].ID)
	}
}

func TestClientToOpenAIRequest(t *testing.T) {
	c, _ := New(Config{Name: "test", BaseURL: "http://localhost/v1"})

	req := protocol.ResponsesRequest{
		Model:        "test-model",
		Instructions: "You are helpful",
		Input: []protocol.ResponseInputItem{
			{Type: "message", Role: "user", Content: []protocol.InputContentPart{{Type: "input_text", Text: "Hello"}}},
		},
	}

	result := c.toOpenAIRequest(req)

	if result["model"] != "test-model" {
		t.Errorf("model = %v, want test-model", result["model"])
	}
	if result["stream"] != true {
		t.Errorf("stream = %v, want true", result["stream"])
	}

	messages := result["messages"].([]map[string]any)
	if len(messages) != 2 {
		t.Errorf("expected 2 messages (system + user), got %d", len(messages))
	}
	if messages[0]["role"] != "system" {
		t.Errorf("first message should be system")
	}
	if messages[1]["role"] != "user" {
		t.Errorf("second message should be user")
	}
}

func TestClientAuthHeaders(t *testing.T) {
	tests := []struct {
		name     string
		auth     config.BackendAuthConfig
		wantAuth string
	}{
		{
			name: "api_key",
			auth: config.BackendAuthConfig{
				Type: "api_key",
				Key:  "test-key",
			},
			wantAuth: "Bearer test-key",
		},
		{
			name: "bearer",
			auth: config.BackendAuthConfig{
				Type: "bearer",
				Key:  "bearer-token",
			},
			wantAuth: "Bearer bearer-token",
		},
		{
			name:     "none",
			auth:     config.BackendAuthConfig{Type: "none"},
			wantAuth: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := New(Config{
				Name:    "test",
				BaseURL: "http://localhost/v1",
				Auth:    tt.auth,
			})
			if err != nil {
				t.Fatalf("New: %v", err)
			}

			req, _ := http.NewRequest("GET", "http://localhost", nil)
			c.applyAuth(req)

			got := req.Header.Get("Authorization")
			if got != tt.wantAuth {
				t.Errorf("Authorization = %q, want %q", got, tt.wantAuth)
			}
		})
	}
}
