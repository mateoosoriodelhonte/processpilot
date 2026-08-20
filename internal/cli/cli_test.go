package cli

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestReadOnlyCommandsUseOnlyFixedGETRoutes(t *testing.T) {
	routes := make(chan string, 4)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		routes <- request.Method + " " + request.URL.RequestURI()
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/api/v1/system":
			_, _ = io.WriteString(writer, `{"data":{"observed":{"totalMemoryBytes":68719476736,"usedMemoryBytes":42949672960,"availableMemoryBytes":25769803776,"totalSwapBytes":8589934592,"usedSwapBytes":1073741824,"cpuPercent":42,"loadAverage1":2,"loadAverage5":1,"loadAverage15":1,"logicalCpuCount":8},"pressure":{"level":"ELEVATED","summary":"Resource use is elevated.","evidence":[]},"timestampUnixMs":1787256000000,"demo":true}}`)
		case "/api/v1/applications":
			_, _ = io.WriteString(writer, `{"data":[{"name":"Ollama","category":"AI_INFERENCE","stoppingRisk":"MEDIUM","processCount":2,"cpuPercent":33.9,"memoryBytes":32641751450,"pids":[301,95707]}],"pagination":{"page":1,"pageSize":20,"totalItems":1,"totalPages":1},"demo":true}`)
		case "/api/v1/processes/95707":
			_, _ = io.WriteString(writer, `{"data":{"observed":{"pid":95707,"parentPid":301,"name":"ollama runner","executable":"/Applications/Ollama.app/Contents/MacOS/ollama","cpuPercent":31.8,"memoryBytes":32212254720,"startTimeUnixSeconds":1787245200,"status":"Run"},"classification":{"application":"Ollama","category":"AI_INFERENCE","stoppingRisk":"MEDIUM","reason":"Local AI runtime.","potentialImpact":"Active inference may stop."},"ownership":{"application":"Ollama","ownerPid":95707,"chain":[95707,301],"confidence":"KNOWN_SIGNATURE"},"explanation":{"text":"Ollama is using a large share of memory.","provider":"ProcessPilot","generatedByAI":false}}}`)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	client := &Client{baseURL: server.URL, httpClient: server.Client()}
	for _, args := range [][]string{{"status"}, {"top"}, {"inspect", "95707"}, {"explain", "95707"}} {
		var output strings.Builder
		if err := client.Run(context.Background(), args, &output); err != nil {
			t.Fatalf("Run(%v) error = %v", args, err)
		}
		if output.Len() == 0 {
			t.Fatalf("Run(%v) produced no output", args)
		}
	}

	want := []string{
		"GET /api/v1/system", "GET /api/v1/applications?page=1&pageSize=20",
		"GET /api/v1/processes/95707", "GET /api/v1/processes/95707",
	}
	for _, expected := range want {
		if got := <-routes; got != expected {
			t.Fatalf("route = %q, want %q", got, expected)
		}
	}
}

func TestRejectsControlAndMalformedCommandsWithoutNetwork(t *testing.T) {
	client := Client{baseURL: "http://127.0.0.1:1", httpClient: &http.Client{Timeout: time.Millisecond}}
	for _, args := range [][]string{{"kill", "1"}, {"stop", "1"}, {"restart", "1"}, {"exec", "whoami"}, {"inspect", "0"}, {"explain", "abc"}, {"top", "extra"}, {}} {
		if err := client.Run(context.Background(), args, io.Discard); err == nil {
			t.Fatalf("Run(%v) unexpectedly succeeded", args)
		}
	}
}

func TestNewIsLoopbackOnly(t *testing.T) {
	client, err := New(7345)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if client.baseURL != "http://127.0.0.1:7345" {
		t.Fatalf("base URL = %q", client.baseURL)
	}
	if _, err := New(80); err == nil {
		t.Fatal("New(80) unexpectedly succeeded")
	}
}

func TestRejectsOversizedLocalAPIResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(writer, strings.Repeat(" ", maximumResponseBytes+1))
	}))
	defer server.Close()
	client := &Client{baseURL: server.URL, httpClient: server.Client()}
	if err := client.Run(context.Background(), []string{"status"}, io.Discard); err == nil {
		t.Fatal("oversized local API response unexpectedly accepted")
	}
}

func TestTerminalOutputStripsControlSequences(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"data":{"observed":{"pid":7,"name":"worker\u001b]52;c;clipboard\u0007","cpuPercent":1,"memoryBytes":1,"startTimeUnixSeconds":1,"status":"Run\u001b[2J"},"classification":{"application":"worker","category":"UNKNOWN","stoppingRisk":"UNKNOWN","reason":"unknown","potentialImpact":"unknown"},"ownership":{"application":"worker","ownerPid":7,"chain":[7],"confidence":"UNKNOWN"},"explanation":{"text":"plain\u001b[31mred","provider":"ProcessPilot","generatedByAI":false}}}`)
	}))
	defer server.Close()
	client := &Client{baseURL: server.URL, httpClient: server.Client()}
	var output strings.Builder
	if err := client.Run(context.Background(), []string{"inspect", "7"}, &output); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if strings.ContainsAny(output.String(), "\x1b\x07") {
		t.Fatalf("terminal output contains control sequence: %q", output.String())
	}
}
