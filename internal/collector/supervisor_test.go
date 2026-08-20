package collector

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/mateoosoriodelhonte/processpilot/internal/protocol"
)

func TestNewSupervisorAllowsOnlyTheTrustedAbsoluteCollector(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		interval time.Duration
	}{
		{name: "relative path", path: "processpilot-collector", interval: 2 * time.Second},
		{name: "wrong binary", path: "/tmp/bash", interval: 2 * time.Second},
		{name: "too frequent", path: "/tmp/processpilot-collector", interval: 499 * time.Millisecond},
		{name: "too slow", path: "/tmp/processpilot-collector", interval: 61 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewSupervisor(tt.path, tt.interval); err == nil {
				t.Fatal("NewSupervisor() error = nil, want rejection")
			}
		})
	}
}

func TestSupervisorBuildsDirectFixedCommandWithoutAShell(t *testing.T) {
	supervisor, err := NewSupervisor("/opt/processpilot/processpilot-collector", 2*time.Second)
	if err != nil {
		t.Fatalf("NewSupervisor() error = %v", err)
	}

	command := supervisor.command(context.Background(), false)

	if command.Path != "/opt/processpilot/processpilot-collector" {
		t.Fatalf("command.Path = %q", command.Path)
	}
	wantArgs := []string{"/opt/processpilot/processpilot-collector", "--interval-ms", "2000"}
	if strings.Join(command.Args, "|") != strings.Join(wantArgs, "|") {
		t.Fatalf("command.Args = %q, want %q", command.Args, wantArgs)
	}
	for _, shell := range []string{"sh", "bash", "zsh", "-c"} {
		if command.Path == shell || contains(command.Args, shell) {
			t.Fatalf("command unexpectedly invokes shell token %q", shell)
		}
	}
}

func TestIngestAcceptsValidLinesAndRejectsMalformedData(t *testing.T) {
	var snapshots []protocol.Snapshot
	valid := validSnapshotJSON()

	err := ingest(strings.NewReader(valid+"\n"), func(snapshot protocol.Snapshot) error {
		snapshots = append(snapshots, snapshot)
		return nil
	})
	if err != nil {
		t.Fatalf("ingest(valid) error = %v", err)
	}
	if len(snapshots) != 1 || snapshots[0].ProtocolVersion != protocol.Version {
		t.Fatalf("snapshots = %#v", snapshots)
	}

	err = ingest(strings.NewReader(`{"protocolVersion":2}`+"\n"), func(protocol.Snapshot) error {
		t.Fatal("handler must not receive invalid snapshot")
		return nil
	})
	if !errors.Is(err, protocol.ErrUnsupportedVersion) {
		t.Fatalf("ingest(version mismatch) error = %v", err)
	}
}

func TestSampleUsesOnlyOnceArgument(t *testing.T) {
	supervisor, err := NewSupervisor("/tmp/processpilot-collector", 2*time.Second)
	if err != nil {
		t.Fatalf("NewSupervisor() error = %v", err)
	}
	supervisor.execCommand = func(_ context.Context, path string, args ...string) *exec.Cmd {
		if path != "/tmp/processpilot-collector" || strings.Join(args, "|") != "--once" {
			t.Fatalf("command = %q %q", path, args)
		}
		return exec.Command("printf", "%s", validSnapshotJSON())
	}

	snapshot, err := supervisor.Sample(context.Background())

	if err != nil {
		t.Fatalf("Sample() error = %v", err)
	}
	if snapshot.ProtocolVersion != protocol.Version {
		t.Fatalf("ProtocolVersion = %d", snapshot.ProtocolVersion)
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func validSnapshotJSON() string {
	return `{"protocolVersion":1,"timestampUnixMs":1787256000000,"sequence":1,"system":{"totalMemoryBytes":100,"usedMemoryBytes":50,"availableMemoryBytes":50,"totalSwapBytes":0,"usedSwapBytes":0,"cpuPercent":10,"loadAverage1":1,"loadAverage5":1,"loadAverage15":1,"logicalCpuCount":8},"processes":[{"pid":2,"parentPid":1,"name":"known","executable":"/Applications/Known.app/Known","cpuPercent":1,"memoryBytes":10,"startTimeUnixSeconds":1787250000,"status":"Run"}]}`
}
