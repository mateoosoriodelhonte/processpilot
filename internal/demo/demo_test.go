package demo

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/mateoosoriodelhonte/processpilot/internal/analysis"
	"github.com/mateoosoriodelhonte/processpilot/internal/protocol"
)

func TestSnapshotIsValidDeterministicAndRepresentative(t *testing.T) {
	anchor := time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC)
	first := Snapshot(anchor, 7)
	second := Snapshot(anchor, 7)
	if first.TimestampUnixMS != uint64(anchor.UnixMilli()) || first.Sequence != 7 {
		t.Fatalf("unexpected identity: %#v", first)
	}
	if len(first.Processes) < 6 || !reflect.DeepEqual(first, second) {
		t.Fatalf("fixture is not representative or deterministic")
	}
	encoded, err := json.Marshal(first)
	if err != nil {
		t.Fatalf("encode snapshot: %v", err)
	}
	if _, err := protocol.Decode(encoded); err != nil {
		t.Fatalf("demo snapshot violates protocol: %v", err)
	}
}

func TestStoreReturnsBoundedFilteredHistory(t *testing.T) {
	anchor := time.Date(2026, time.August, 20, 12, 0, 0, 0, time.UTC)
	store := NewStore(anchor)
	history, err := store.ApplicationHistory(context.Background(), "Ollama", anchor.Add(-time.Hour), 2)
	if err != nil {
		t.Fatalf("ApplicationHistory() error = %v", err)
	}
	if len(history) != 2 || history[0].Application != "Ollama" {
		t.Fatalf("unexpected history: %#v", history)
	}
	if err := store.Record(context.Background(), Snapshot(anchor, 1), analysis.Result{}); err != nil {
		t.Fatalf("Record() error = %v", err)
	}
}
