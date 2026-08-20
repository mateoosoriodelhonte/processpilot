package security_test

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/mateoosoriodelhonte/processpilot/internal/ai"
	"github.com/mateoosoriodelhonte/processpilot/internal/analysis"
	"github.com/mateoosoriodelhonte/processpilot/internal/app"
	"github.com/mateoosoriodelhonte/processpilot/internal/demo"
	"github.com/mateoosoriodelhonte/processpilot/internal/protocol"
	"github.com/mateoosoriodelhonte/processpilot/internal/store"
	"github.com/mateoosoriodelhonte/processpilot/internal/web"
	_ "modernc.org/sqlite"
)

func TestDashboardAddressCannotBecomePublic(t *testing.T) {
	address, err := web.Address(web.DefaultPort)
	if err != nil {
		t.Fatalf("Address() error = %v", err)
	}
	if address != "127.0.0.1:7345" {
		t.Fatalf("default address = %q", address)
	}
	for _, port := range []int{-1, 0, 80, 65536} {
		if _, err := web.Address(port); err == nil {
			t.Fatalf("Address(%d) unexpectedly succeeded", port)
		}
	}
}

func TestNoCommandControlOrFilesystemAPIExists(t *testing.T) {
	handler := securityHandler(t)
	requests := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/kill"},
		{http.MethodPost, "/api/v1/processes/95707"},
		{http.MethodPost, "/api/v1/processes/95707/stop"},
		{http.MethodPost, "/api/v1/restart"},
		{http.MethodPost, "/api/v1/exec"},
		{http.MethodGet, "/api/v1/files/etc/passwd"},
		{http.MethodGet, "/api/v1/fs?path=/etc/passwd"},
		{http.MethodGet, "/api/v1/processes/95707?path=/etc/passwd"},
		{http.MethodGet, "/static/%2e%2e/go.mod"},
	}
	for _, request := range requests {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(request.method, "http://127.0.0.1"+request.path, nil))
		if recorder.Code >= 200 && recorder.Code < 300 {
			t.Fatalf("%s %s exposed status %d: %s", request.method, request.path, recorder.Code, recorder.Body.String())
		}
		if strings.Contains(recorder.Body.String(), "module github.com/") {
			t.Fatalf("%s exposed a repository file", request.path)
		}
	}
}

func TestTelemetrySchemaCannotCarryOrPersistRawCommands(t *testing.T) {
	processType := reflect.TypeOf(protocol.ProcessSample{})
	for index := 0; index < processType.NumField(); index++ {
		name := strings.ToLower(processType.Field(index).Name)
		for _, prohibited := range []string{"command", "argument", "environment", "credential", "secret", "token"} {
			if strings.Contains(name, prohibited) {
				t.Fatalf("collector protocol exposes prohibited field %q", processType.Field(index).Name)
			}
		}
	}

	path := filepath.Join(t.TempDir(), "security.db")
	telemetryStore, err := store.Open(path, store.DefaultRetention)
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	if err := telemetryStore.Close(); err != nil {
		t.Fatalf("store.Close() error = %v", err)
	}
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	defer database.Close()
	rows, err := database.Query("SELECT COALESCE(sql, '') FROM sqlite_master")
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	defer rows.Close()
	var schema strings.Builder
	for rows.Next() {
		var statement string
		if err := rows.Scan(&statement); err != nil {
			t.Fatalf("scan schema: %v", err)
		}
		schema.WriteString(strings.ToLower(statement))
	}
	for _, prohibited := range []string{"command_line", "commandline", "environment", "credential", "password", "secret", "api_key"} {
		if strings.Contains(schema.String(), prohibited) {
			t.Fatalf("SQLite schema persists prohibited field %q", prohibited)
		}
	}
}

func TestAIInputIsAllowlistedAndHasNoSystemAuthority(t *testing.T) {
	inputType := reflect.TypeOf(ai.Input{})
	for index := 0; index < inputType.NumField(); index++ {
		name := strings.ToLower(inputType.Field(index).Name)
		for _, prohibited := range []string{"pid", "command", "path", "file", "user", "environment", "credential", "token"} {
			if strings.Contains(name, prohibited) {
				t.Fatalf("AI input exposes prohibited field %q", inputType.Field(index).Name)
			}
		}
	}
	providerType := reflect.TypeOf((*ai.Provider)(nil)).Elem()
	if providerType.NumMethod() != 1 || providerType.Method(0).Name != "Explain" {
		t.Fatalf("AI provider exposes authority beyond Explain: %v", providerType)
	}
	for _, path := range []string{"internal/ai/provider.go", "internal/ai/ollama.go"} {
		source := readRepositoryFile(t, path)
		for _, prohibited := range []string{"os/exec", "syscall", "process.kill", "net.listen"} {
			if strings.Contains(strings.ToLower(source), prohibited) {
				t.Fatalf("AI implementation %s contains authority %q", path, prohibited)
			}
		}
	}
}

func TestMalformedAndVersionMismatchedCollectorDataFailsClosed(t *testing.T) {
	for _, raw := range [][]byte{nil, []byte("{"), []byte(strings.Repeat("x", protocol.MaxLineBytes+1))} {
		if _, err := protocol.Decode(raw); err == nil {
			t.Fatalf("malformed collector data was accepted")
		}
	}
	raw := readRepositoryFile(t, "testdata/protocol-v1.json")
	raw = strings.Replace(raw, `"protocolVersion": 1`, `"protocolVersion": 999`, 1)
	_, err := protocol.Decode([]byte(raw))
	if !errors.Is(err, protocol.ErrUnsupportedVersion) {
		t.Fatalf("version mismatch error = %v", err)
	}
}

func TestUnknownProcessesRemainConservative(t *testing.T) {
	classification := analysis.Classify(protocol.ProcessSample{Name: "mystery-worker"})
	if classification.Category != analysis.CategoryUnknown || classification.Risk != analysis.RiskUnknown {
		t.Fatalf("unknown process was guessed: %#v", classification)
	}
}

func TestProductionSourcesContainNoPrivilegeOrProcessControlCalls(t *testing.T) {
	root := repositoryRoot(t)
	productionRoots := []string{"cmd", "internal", filepath.Join("collector", "src")}
	prohibited := []string{
		"process.kill(", "syscall.kill(", "findprocess(", "std::process::command", "command::new(",
		"libc::kill(", "setuid(", "seteuid(", "geteuid(", "sudo ", "authorizationexecutewithprivileges", "smjobbless",
		"cgeventtap", "avcapture", "screencapturekit", "seckeychain", "pcap_open_live",
		"0.0.0.0", `"[::]"`,
	}
	for _, relativeRoot := range productionRoots {
		err := filepath.WalkDir(filepath.Join(root, relativeRoot), func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || strings.HasSuffix(path, "_test.go") || (!strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, ".rs")) {
				return nil
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			lower := strings.ToLower(string(raw))
			for _, fragment := range prohibited {
				if strings.Contains(lower, fragment) {
					t.Fatalf("production source %s contains prohibited authority %q", path, fragment)
				}
			}
			if strings.Contains(lower, `"os/exec"`) && filepath.ToSlash(path) != filepath.ToSlash(filepath.Join(root, "internal/collector/supervisor.go")) {
				t.Fatalf("only the fixed trusted collector supervisor may import os/exec: %s", path)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("scan production source: %v", err)
		}
	}
}

func securityHandler(t *testing.T) http.Handler {
	t.Helper()
	anchor := time.Now().UTC()
	service := app.NewService(nil, ai.NoAIProvider{}, true)
	if err := service.Accept(context.Background(), demo.Snapshot(anchor, 1)); err != nil {
		t.Fatalf("Accept() error = %v", err)
	}
	return web.New(service).Handler()
}

func readRepositoryFile(t *testing.T, relative string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), filepath.FromSlash(relative)))
	if err != nil {
		t.Fatalf("read %s: %v", relative, err)
	}
	return string(raw)
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, current, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate security test source")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(current), "..", ".."))
}
