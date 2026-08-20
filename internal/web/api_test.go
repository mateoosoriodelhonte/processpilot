package web

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mateoosoriodelhonte/processpilot/internal/ai"
	"github.com/mateoosoriodelhonte/processpilot/internal/app"
	"github.com/mateoosoriodelhonte/processpilot/internal/protocol"
)

func TestDefaultAddressIsLoopbackOnly(t *testing.T) {
	address, err := Address(7345)
	if err != nil {
		t.Fatalf("Address() error = %v", err)
	}
	if address != "127.0.0.1:7345" || strings.Contains(address, "0.0.0.0") {
		t.Fatalf("Address() = %q", address)
	}
	for _, port := range []int{0, 80, 65_536} {
		if _, err := Address(port); err == nil {
			t.Fatalf("Address(%d) error = nil", port)
		}
	}
}

func TestReadOnlyAPIRoutesExposeSeparatedEvidence(t *testing.T) {
	handler := testHandler(t)
	for _, path := range []string{
		"/api/v1/system",
		"/api/v1/processes",
		"/api/v1/processes/10",
		"/api/v1/applications",
		"/api/v1/history",
		"/api/v1/anomalies",
	} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, localRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d body=%s", path, recorder.Code, recorder.Body.String())
		}
		if recorder.Header().Get("X-Content-Type-Options") != "nosniff" || recorder.Header().Get("Content-Security-Policy") == "" {
			t.Fatalf("GET %s lacks security headers: %v", path, recorder.Header())
		}
		lower := strings.ToLower(recorder.Body.String())
		for _, prohibited := range []string{"commandline", "rawcommand", "environment"} {
			if strings.Contains(lower, prohibited) {
				t.Fatalf("GET %s leaked %q: %s", path, prohibited, recorder.Body.String())
			}
		}
	}

	detail := httptest.NewRecorder()
	handler.ServeHTTP(detail, localRequest(http.MethodGet, "/api/v1/processes/10", nil))
	var response map[string]any
	if err := json.Unmarshal(detail.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	data := response["data"].(map[string]any)
	if data["observed"] == nil || data["classification"] == nil || data["explanation"] == nil {
		t.Fatalf("detail does not separate evidence: %#v", data)
	}
}

func TestNoCommandProcessControlOrFilesystemRouteExists(t *testing.T) {
	handler := testHandler(t)
	for _, path := range []string{
		"/api/v1/exec",
		"/api/v1/commands",
		"/api/v1/processes/10/kill",
		"/api/v1/processes/10/stop",
		"/api/v1/files/etc/passwd",
	} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, localRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("GET %s status = %d, want 404", path, recorder.Code)
		}
	}

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, localRequest(http.MethodPost, "/api/v1/processes/10", strings.NewReader(`{"action":"kill"}`)))
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST process status = %d, want 405", recorder.Code)
	}

	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, localRequest(http.MethodGet, "/api/v1/history?path=/etc/passwd", nil))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("filesystem-like query status = %d, want 400", recorder.Code)
	}
}

func TestMalformedAPIInputFailsWithConsistentErrors(t *testing.T) {
	handler := testHandler(t)
	for _, path := range []string{
		"/api/v1/processes/not-a-pid",
		"/api/v1/processes?page=0",
		"/api/v1/applications?pageSize=1000000",
		"/api/v1/history?since=not-a-time",
	} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, localRequest(http.MethodGet, path, nil))
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("GET %s status = %d body=%s", path, recorder.Code, recorder.Body.String())
		}
		var response struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil || response.Error.Code == "" || response.Error.Message == "" {
			t.Fatalf("inconsistent error for %s: %s (%v)", path, recorder.Body.String(), err)
		}
	}
}

func TestPaginationSafelyHandlesAPlatformMaximumPage(t *testing.T) {
	recorder := httptest.NewRecorder()
	testHandler(t).ServeHTTP(recorder, localRequest(http.MethodGet, "/api/v1/processes?page=9223372036854775807&pageSize=200", nil))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"data":[]`) {
		t.Fatalf("status = %d body=%s, want an empty bounded page", recorder.Code, recorder.Body.String())
	}
}

func TestRejectsNonLoopbackHostAndCrossOriginRequests(t *testing.T) {
	handler := testHandler(t)
	tests := []struct {
		name   string
		mutate func(*http.Request)
	}{
		{name: "non-loopback host", mutate: func(request *http.Request) { request.Host = "attacker.example" }},
		{name: "cross-origin", mutate: func(request *http.Request) { request.Header.Set("Origin", "https://attacker.example") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := localRequest(http.MethodGet, "/api/v1/system", nil)
			tt.mutate(request)
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusMisdirectedRequest {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusMisdirectedRequest)
			}
		})
	}
}

func localRequest(method, target string, body io.Reader) *http.Request {
	return httptest.NewRequest(method, "http://127.0.0.1"+target, body)
}

func testHandler(t *testing.T) http.Handler {
	t.Helper()
	service := app.NewService(nil, ai.NoAIProvider{}, true)
	if err := service.Accept(context.Background(), webSnapshot()); err != nil {
		t.Fatalf("Accept() error = %v", err)
	}
	return New(service).Handler()
}

func webSnapshot() protocol.Snapshot {
	executable := "/Applications/Ollama.app/Contents/MacOS/ollama"
	now := time.Now().UTC()
	return protocol.Snapshot{
		ProtocolVersion: protocol.Version, TimestampUnixMS: uint64(now.UnixMilli()), Sequence: 1,
		System: protocol.SystemSample{
			TotalMemoryBytes: 64 << 30, UsedMemoryBytes: 40 << 30, AvailableMemoryBytes: 24 << 30,
			TotalSwapBytes: 8 << 30, UsedSwapBytes: 1 << 30, CPUPercent: 42, LogicalCPUCount: 8,
		},
		Processes: []protocol.ProcessSample{{
			PID: 10, Name: "ollama runner", Executable: &executable, CPUPercent: 4.2,
			MemoryBytes: 30 << 30, StartTimeUnixSeconds: uint64(now.Add(-time.Hour).Unix()), Status: "Run",
		}},
	}
}
