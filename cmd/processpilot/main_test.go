package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mateoosoriodelhonte/processpilot/internal/store"
	"github.com/mateoosoriodelhonte/processpilot/internal/web"
)

func TestServerConfigurationDefaultsArePrivateAndBounded(t *testing.T) {
	config, err := parseServerConfig(modeLive, nil)
	if err != nil {
		t.Fatalf("parseServerConfig() error = %v", err)
	}
	if config.port != web.DefaultPort || config.interval != 2*time.Second || config.retention != store.DefaultRetention {
		t.Fatalf("defaults = %#v", config)
	}
	if config.mode != modeLive || config.ollamaModel != "" {
		t.Fatalf("unsafe default configuration = %#v", config)
	}
}

func TestServerConfigurationRejectsUnsafeOrAmbiguousOptions(t *testing.T) {
	tests := [][]string{
		{"--port", "80"},
		{"--interval", "100ms"},
		{"--retention", "31d"},
		{"--ollama-model", "gemma3", "--ollama-endpoint", "http://192.168.1.2:11434"},
		{"--ollama-endpoint", "http://127.0.0.1:11435"},
		{"unexpected"},
		{"--host", "0.0.0.0"},
	}
	for _, args := range tests {
		if _, err := parseServerConfig(modeLive, args); err == nil {
			t.Fatalf("parseServerConfig(%v) unexpectedly succeeded", args)
		}
	}
	if _, err := parseServerConfig(modeDemo, []string{"--ollama-model", "gemma3"}); err == nil {
		t.Fatal("demo mode unexpectedly accepted nondeterministic AI")
	}
}

func TestCollectorPathCanOnlyResolveToPackagedSibling(t *testing.T) {
	directory := t.TempDir()
	executable := filepath.Join(directory, "processpilot")
	collector := filepath.Join(directory, "processpilot-collector")
	for _, path := range []string{executable, collector} {
		if err := os.WriteFile(path, []byte("test"), 0o700); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
	}
	resolved, err := resolveCollectorPath(executable)
	if err != nil {
		t.Fatalf("resolveCollectorPath() error = %v", err)
	}
	want, err := filepath.EvalSymlinks(collector)
	if err != nil {
		t.Fatalf("resolve fixture: %v", err)
	}
	if resolved != want {
		t.Fatalf("collector path = %q, want %q", resolved, want)
	}
	if err := os.Remove(collector); err != nil {
		t.Fatalf("remove fixture: %v", err)
	}
	if _, err := resolveCollectorPath(executable); err == nil {
		t.Fatal("missing collector unexpectedly accepted")
	}
}

func TestCommandSurfaceContainsNoProcessControl(t *testing.T) {
	for _, command := range []string{"status", "top", "inspect", "explain"} {
		if !isCLICommand(command) {
			t.Fatalf("read-only command %q is missing", command)
		}
	}
	for _, command := range []string{"kill", "stop", "restart", "exec", "run"} {
		if isCLICommand(command) {
			t.Fatalf("control command %q is exposed", command)
		}
	}
}
