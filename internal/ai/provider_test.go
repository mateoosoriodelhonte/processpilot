package ai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mateoosoriodelhonte/processpilot/internal/analysis"
)

func TestNoAIProviderExplainsWithoutARequiredModel(t *testing.T) {
	provider := NoAIProvider{}

	explanation, err := provider.Explain(context.Background(), testInput())

	if err != nil {
		t.Fatalf("Explain() error = %v", err)
	}
	if explanation.Provider != "ProcessPilot" || explanation.GeneratedByAI {
		t.Fatalf("explanation = %#v", explanation)
	}
	if !strings.Contains(explanation.Text, "Ollama") || !strings.Contains(explanation.Text, "memory") {
		t.Fatalf("explanation is not useful: %q", explanation.Text)
	}
}

func TestFallbackProviderDegradesToDeterministicExplanation(t *testing.T) {
	provider := FallbackProvider{Primary: errorProvider{}, Fallback: NoAIProvider{}}
	explanation, err := provider.Explain(context.Background(), testInput())
	if err != nil {
		t.Fatalf("Explain() error = %v", err)
	}
	if explanation.Provider != "ProcessPilot" || explanation.GeneratedByAI {
		t.Fatalf("explanation = %#v", explanation)
	}
}

type errorProvider struct{}

func (errorProvider) Explain(context.Context, Input) (Explanation, error) {
	return Explanation{}, errors.New("unavailable")
}

func TestExplanationInputCannotCarrySensitiveMetadata(t *testing.T) {
	inputType := reflect.TypeOf(Input{})
	for index := 0; index < inputType.NumField(); index++ {
		name := strings.ToLower(inputType.Field(index).Name)
		for _, prohibited := range []string{"command", "path", "user", "credential", "environment", "file", "pid", "token"} {
			if strings.Contains(name, prohibited) {
				t.Fatalf("Input exposes prohibited field %q", inputType.Field(index).Name)
			}
		}
	}
}

func TestOllamaReceivesOnlyAllowlistedSanitizedMetadata(t *testing.T) {
	var requestBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/generate" || request.Method != http.MethodPost {
			t.Fatalf("request = %s %s", request.Method, request.URL.Path)
		}
		if err := json.NewDecoder(request.Body).Decode(&requestBody); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"response":"Ollama is using memory for a local model.","done":true}`))
	}))
	defer server.Close()

	provider, err := NewOllamaProvider(server.URL, "gemma3", &http.Client{Timeout: time.Second})
	if err != nil {
		t.Fatalf("NewOllamaProvider() error = %v", err)
	}
	explanation, err := provider.Explain(context.Background(), testInput())
	if err != nil {
		t.Fatalf("Explain() error = %v", err)
	}
	if !explanation.GeneratedByAI || explanation.Provider != "Ollama" {
		t.Fatalf("explanation = %#v", explanation)
	}

	encoded, err := json.Marshal(requestBody)
	if err != nil {
		t.Fatalf("marshal captured request: %v", err)
	}
	lower := strings.ToLower(string(encoded))
	for _, prohibited := range []string{"commandline", "/users/", "username", "password", "token", "environment", "pid"} {
		if strings.Contains(lower, prohibited) {
			t.Fatalf("Ollama payload contains prohibited metadata %q: %s", prohibited, encoded)
		}
	}
	if requestBody["stream"] != false {
		t.Fatalf("stream = %#v, want false", requestBody["stream"])
	}
}

func TestOllamaRejectsNonLoopbackAndInvalidConfiguration(t *testing.T) {
	for _, endpoint := range []string{
		"https://example.com",
		"http://192.168.1.4:11434",
		"http://user:pass@127.0.0.1:11434",
		"file:///tmp/ollama.sock",
	} {
		if _, err := NewOllamaProvider(endpoint, "gemma3", nil); err == nil {
			t.Fatalf("endpoint %q was accepted", endpoint)
		}
	}
	if _, err := NewOllamaProvider("http://127.0.0.1:11434", "bad model; command", nil); err == nil {
		t.Fatal("unsafe model name was accepted")
	}
}

func TestModelOutputRemainsInertText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(`{"response":"kill -9 1 && rm -rf /","done":true}`))
	}))
	defer server.Close()
	provider, err := NewOllamaProvider(server.URL, "gemma3", &http.Client{Timeout: time.Second})
	if err != nil {
		t.Fatalf("NewOllamaProvider() error = %v", err)
	}

	explanation, err := provider.Explain(context.Background(), testInput())

	if err != nil {
		t.Fatalf("Explain() error = %v", err)
	}
	if explanation.Text != "kill -9 1 && rm -rf /" {
		t.Fatalf("model output changed unexpectedly: %q", explanation.Text)
	}
	if reflect.TypeOf(provider).NumMethod() != 1 {
		t.Fatalf("Ollama provider exposes authority beyond Explain: %d methods", reflect.TypeOf(provider).NumMethod())
	}
}

func testInput() Input {
	return Input{
		Application: "Ollama", Category: analysis.CategoryAIInference, Risk: analysis.RiskMedium,
		CPUPercent: 4.2, MemoryBytes: 30 << 30, TotalMemoryBytes: 64 << 30,
		DeterministicReason: "The executable identity matches a local AI model runtime.",
		PotentialImpact:     "Stopping it may interrupt active local model inference.",
	}
}
