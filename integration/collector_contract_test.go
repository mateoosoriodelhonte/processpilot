//go:build integration

package integration_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/mateoosoriodelhonte/processpilot/internal/collector"
	"github.com/mateoosoriodelhonte/processpilot/internal/protocol"
)

func TestRealRustCollectorDeserializesInGo(t *testing.T) {
	collectorPath, err := filepath.Abs("../target/debug/processpilot-collector")
	if err != nil {
		t.Fatalf("resolve collector path: %v", err)
	}
	supervisor, err := collector.NewSupervisor(collectorPath, 2*time.Second)
	if err != nil {
		t.Fatalf("NewSupervisor() error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	snapshot, err := supervisor.Sample(ctx)

	if err != nil {
		t.Fatalf("Sample() error = %v; build the collector before this test", err)
	}
	if snapshot.ProtocolVersion != protocol.Version {
		t.Fatalf("ProtocolVersion = %d, want %d", snapshot.ProtocolVersion, protocol.Version)
	}
	if snapshot.System.TotalMemoryBytes == 0 || len(snapshot.Processes) == 0 {
		t.Fatalf("real snapshot is unexpectedly empty: %#v", snapshot.System)
	}
}
