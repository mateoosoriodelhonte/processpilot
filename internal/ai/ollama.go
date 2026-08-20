package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultOllamaTimeout    = 5 * time.Second
	maximumResponseBytes    = 64 * 1024
	maximumExplanationBytes = 4_000
)

type OllamaProvider struct {
	generateURL string
	model       string
	client      *http.Client
}

func NewOllamaProvider(endpoint, model string, client *http.Client) (*OllamaProvider, error) {
	base, err := validateLoopbackEndpoint(endpoint)
	if err != nil {
		return nil, err
	}
	if !validModel(model) {
		return nil, errors.New("Ollama model name contains unsupported characters")
	}

	if client == nil {
		client = &http.Client{Timeout: defaultOllamaTimeout}
	} else {
		copy := *client
		client = &copy
		if client.Timeout <= 0 || client.Timeout > 30*time.Second {
			client.Timeout = defaultOllamaTimeout
		}
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}

	return &OllamaProvider{
		generateURL: base.ResolveReference(&url.URL{Path: "/api/generate"}).String(),
		model:       model,
		client:      client,
	}, nil
}

func (provider *OllamaProvider) Explain(ctx context.Context, input Input) (Explanation, error) {
	if err := input.validate(); err != nil {
		return Explanation{}, err
	}
	prompt := fmt.Sprintf(
		"Explain this observed resource usage to a nontechnical Mac user in two concise sentences. Do not give commands and do not claim an action is safe. Application: %s. Category: %s. Stopping risk: %s. CPU: %.1f percent. Memory: %d bytes of %d bytes. Deterministic reason: %s. Potential impact: %s.",
		input.Application, input.Category, input.Risk, input.CPUPercent, input.MemoryBytes,
		input.TotalMemoryBytes, input.DeterministicReason, input.PotentialImpact,
	)
	payload := struct {
		Model   string `json:"model"`
		Prompt  string `json:"prompt"`
		Stream  bool   `json:"stream"`
		Options struct {
			Temperature float64 `json:"temperature"`
			NumPredict  int     `json:"num_predict"`
		} `json:"options"`
	}{Model: provider.model, Prompt: prompt, Stream: false}
	payload.Options.Temperature = 0.2
	payload.Options.NumPredict = 180
	encoded, err := json.Marshal(payload)
	if err != nil {
		return Explanation{}, fmt.Errorf("encode Ollama request: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, provider.generateURL, bytes.NewReader(encoded))
	if err != nil {
		return Explanation{}, fmt.Errorf("create Ollama request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := provider.client.Do(request)
	if err != nil {
		return Explanation{}, fmt.Errorf("Ollama is unavailable: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Explanation{}, fmt.Errorf("Ollama returned HTTP %d", response.StatusCode)
	}

	raw, err := io.ReadAll(io.LimitReader(response.Body, maximumResponseBytes+1))
	if err != nil {
		return Explanation{}, fmt.Errorf("read Ollama response: %w", err)
	}
	if len(raw) > maximumResponseBytes {
		return Explanation{}, errors.New("Ollama response exceeds size limit")
	}
	var result struct {
		Response string `json:"response"`
		Done     bool   `json:"done"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return Explanation{}, fmt.Errorf("decode Ollama response: %w", err)
	}
	result.Response = strings.TrimSpace(result.Response)
	if !result.Done || result.Response == "" || len(result.Response) > maximumExplanationBytes {
		return Explanation{}, errors.New("Ollama returned an incomplete or invalid explanation")
	}
	return Explanation{Text: result.Response, Provider: "Ollama", GeneratedByAI: true}, nil
}

func validateLoopbackEndpoint(raw string) (*url.URL, error) {
	endpoint, err := url.Parse(raw)
	if err != nil || endpoint.Scheme != "http" || endpoint.Host == "" {
		return nil, errors.New("Ollama endpoint must be an HTTP loopback URL")
	}
	if endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" || (endpoint.Path != "" && endpoint.Path != "/") {
		return nil, errors.New("Ollama endpoint must not include credentials, a path, query, or fragment")
	}

	hostname := endpoint.Hostname()
	if hostname == "localhost" {
		hostname = "127.0.0.1"
	} else {
		address := net.ParseIP(hostname)
		if address == nil || !address.IsLoopback() {
			return nil, errors.New("Ollama endpoint must use a loopback address")
		}
	}
	if port := endpoint.Port(); port != "" {
		endpoint.Host = net.JoinHostPort(hostname, port)
	} else {
		endpoint.Host = hostname
	}
	endpoint.Path = ""
	return endpoint, nil
}

func validModel(model string) bool {
	if model == "" || len(model) > 128 {
		return false
	}
	for _, character := range model {
		if !(character >= 'a' && character <= 'z') && !(character >= 'A' && character <= 'Z') && !(character >= '0' && character <= '9') && !strings.ContainsRune("-_.:", character) {
			return false
		}
	}
	return true
}
