package web

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mateoosoriodelhonte/processpilot/internal/ai"
	"github.com/mateoosoriodelhonte/processpilot/internal/app"
)

func TestDashboardPagesRenderSemanticDemoLabeledContent(t *testing.T) {
	handler := testHandler(t)
	tests := []struct {
		path string
		text string
	}{
		{path: "/", text: "Why is my Mac slow?"},
		{path: "/applications", text: "Applications"},
		{path: "/processes", text: "Raw processes"},
		{path: "/processes/10", text: "ProcessPilot classification"},
		{path: "/history", text: "Resource history"},
		{path: "/anomalies", text: "Anomalies"},
		{path: "/privacy", text: "What ProcessPilot cannot see"},
		{path: "/settings", text: "Local settings"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, tt.path, nil))
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d body=%s", recorder.Code, recorder.Body.String())
			}
			body := recorder.Body.String()
			for _, required := range []string{"<main", "<h1", tt.text, "Demo data"} {
				if !strings.Contains(body, required) {
					t.Fatalf("page lacks %q: %s", required, body)
				}
			}
			if strings.Contains(body, "<button") || strings.Contains(body, ">Kill<") || strings.Contains(body, ">Stop Process<") {
				t.Fatalf("page contains action control: %s", body)
			}
		})
	}
}

func TestProcessDetailSeparatesEvidenceClassificationAndExplanation(t *testing.T) {
	recorder := httptest.NewRecorder()
	testHandler(t).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/processes/10", nil))
	body := recorder.Body.String()

	observed := strings.Index(body, ">Observed<")
	classified := strings.Index(body, ">ProcessPilot classification<")
	explained := strings.Index(body, ">Explanation<")
	if observed < 0 || classified < 0 || explained < 0 || !(observed < classified && classified < explained) {
		t.Fatalf("detail sections are missing or blurred: %s", body)
	}
}

func TestPrivacyPageStatesTheConcreteBoundary(t *testing.T) {
	recorder := httptest.NewRecorder()
	testHandler(t).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/privacy", nil))
	lower := strings.ToLower(recorder.Body.String())

	for _, phrase := range []string{
		"process names", "cpu", "ram", "passwords", "keychain", "browser history",
		"file contents", "ram contents", "keystrokes", "microphone", "camera", "network packet contents",
		"does not require administrator privileges", "does not upload",
	} {
		if !strings.Contains(lower, phrase) {
			t.Fatalf("privacy page lacks %q", phrase)
		}
	}
}

func TestSettingsPageReflectsEffectiveRuntimeConfiguration(t *testing.T) {
	service := app.NewService(nil, ai.NoAIProvider{}, true)
	if err := service.Accept(context.Background(), webSnapshot()); err != nil {
		t.Fatalf("Accept() error = %v", err)
	}
	handler := NewWithSettings(service, Settings{
		Interval: 3 * time.Second, Retention: 2 * 24 * time.Hour,
		ExplanationProvider: "Ollama with NoAI fallback",
	}).Handler()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/settings", nil))
	for _, expected := range []string{"3s", "2 days", "Ollama with NoAI fallback", "127.0.0.1"} {
		if !strings.Contains(recorder.Body.String(), expected) {
			t.Fatalf("settings page lacks %q: %s", expected, recorder.Body.String())
		}
	}
}

func TestStaticAssetsAreLocalAndCSPCompatible(t *testing.T) {
	for _, path := range []string{"/static/style.css", "/static/app.js"} {
		recorder := httptest.NewRecorder()
		testHandler(t).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusOK || recorder.Body.Len() == 0 {
			t.Fatalf("GET %s status=%d size=%d", path, recorder.Code, recorder.Body.Len())
		}
		if strings.Contains(recorder.Body.String(), "https://") || strings.Contains(recorder.Body.String(), "http://") {
			t.Fatalf("asset %s contains remote dependency", path)
		}
	}
}

func TestSSEPublishesBoundedSanitizedSummary(t *testing.T) {
	service := app.NewService(nil, ai.NoAIProvider{}, true)
	if err := service.Accept(context.Background(), webSnapshot()); err != nil {
		t.Fatalf("Accept() error = %v", err)
	}
	server := httptest.NewServer(New(service).Handler())
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/api/v1/events", nil)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("GET events: %v", err)
	}
	defer response.Body.Close()
	reader := bufio.NewReader(response.Body)
	event, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read event type: %v", err)
	}
	data, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		t.Fatalf("read event data: %v", err)
	}
	if event != "event: snapshot\n" || !strings.HasPrefix(data, "data: {") {
		t.Fatalf("unexpected SSE: %q %q", event, data)
	}
	lower := strings.ToLower(data)
	for _, prohibited := range []string{"commandline", "executable", "parentpid", "environment", "path"} {
		if strings.Contains(lower, prohibited) {
			t.Fatalf("SSE leaked process metadata %q: %s", prohibited, data)
		}
	}
}
