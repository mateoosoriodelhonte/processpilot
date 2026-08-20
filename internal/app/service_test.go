package app

import (
	"context"
	"testing"
	"time"

	"github.com/mateoosoriodelhonte/processpilot/internal/ai"
	"github.com/mateoosoriodelhonte/processpilot/internal/analysis"
	"github.com/mateoosoriodelhonte/processpilot/internal/protocol"
)

func TestAcceptBuildsPersistsAndPublishesReadOnlyState(t *testing.T) {
	store := &fakeStore{}
	service := NewService(store, ai.NoAIProvider{}, false)
	updates, cancel, err := service.Subscribe()
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	defer cancel()
	snapshot := serviceSnapshot()

	if err := service.Accept(context.Background(), snapshot); err != nil {
		t.Fatalf("Accept() error = %v", err)
	}
	state, ok := service.Current()
	if !ok {
		t.Fatal("Current() has no state")
	}
	if state.Demo || len(state.Analysis.Applications) != 1 || state.Pressure.Level == "" {
		t.Fatalf("state = %#v", state)
	}
	if store.records != 1 || store.cleanups != 1 {
		t.Fatalf("store records=%d cleanups=%d", store.records, store.cleanups)
	}
	select {
	case update := <-updates:
		if update.Snapshot.Sequence != snapshot.Sequence {
			t.Fatalf("update sequence = %d", update.Snapshot.Sequence)
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber did not receive state")
	}
}

func TestExplainUsesOnlyTheExplanationProvider(t *testing.T) {
	service := NewService(&fakeStore{}, ai.NoAIProvider{}, false)
	if err := service.Accept(context.Background(), serviceSnapshot()); err != nil {
		t.Fatalf("Accept() error = %v", err)
	}

	explanation, err := service.Explain(context.Background(), 10)

	if err != nil {
		t.Fatalf("Explain() error = %v", err)
	}
	if explanation.GeneratedByAI || explanation.Text == "" {
		t.Fatalf("explanation = %#v", explanation)
	}
	if _, err := service.Explain(context.Background(), 999); err == nil {
		t.Fatal("Explain(unknown PID) error = nil")
	}
}

func TestDemoStateIsExplicitlyLabeled(t *testing.T) {
	service := NewService(nil, ai.NoAIProvider{}, true)

	if err := service.Accept(context.Background(), serviceSnapshot()); err != nil {
		t.Fatalf("Accept() error = %v", err)
	}
	state, _ := service.Current()
	if !state.Demo {
		t.Fatal("demo state is not labeled")
	}
}

func TestCurrentReturnsAnImmutableCopy(t *testing.T) {
	service := NewService(nil, ai.NoAIProvider{}, false)
	if err := service.Accept(context.Background(), serviceSnapshot()); err != nil {
		t.Fatalf("Accept() error = %v", err)
	}
	first, _ := service.Current()
	first.Analysis.Processes[0].Observed.Name = "tampered"
	*first.Analysis.Processes[0].Observed.Executable = "/private/tampered"
	first.Analysis.Processes[0].Ownership.Chain[0] = 999

	second, _ := service.Current()

	if second.Analysis.Processes[0].Observed.Name == "tampered" || *second.Analysis.Processes[0].Observed.Executable == "/private/tampered" || second.Analysis.Processes[0].Ownership.Chain[0] == 999 {
		t.Fatalf("Current() leaked mutable state: %#v", second.Analysis.Processes[0])
	}
}

type fakeStore struct {
	records  int
	cleanups int
	history  []analysis.HistoryPoint
}

func (store *fakeStore) Record(context.Context, protocol.Snapshot, analysis.Result) error {
	store.records++
	return nil
}

func (store *fakeStore) Cleanup(context.Context, time.Time) (int64, error) {
	store.cleanups++
	return 0, nil
}

func (store *fakeStore) AllApplicationHistory(context.Context, time.Time, int) ([]analysis.HistoryPoint, error) {
	return store.history, nil
}

func (store *fakeStore) ApplicationHistory(context.Context, string, time.Time, int) ([]analysis.HistoryPoint, error) {
	return store.history, nil
}

func serviceSnapshot() protocol.Snapshot {
	executable := "/Applications/Ollama.app/Contents/MacOS/ollama"
	now := time.Now().UTC()
	return protocol.Snapshot{
		ProtocolVersion: protocol.Version, TimestampUnixMS: uint64(now.UnixMilli()), Sequence: 1,
		System: protocol.SystemSample{
			TotalMemoryBytes: 64 << 30, UsedMemoryBytes: 40 << 30, AvailableMemoryBytes: 24 << 30,
			TotalSwapBytes: 8 << 30, UsedSwapBytes: 1 << 30, CPUPercent: 20, LogicalCPUCount: 8,
		},
		Processes: []protocol.ProcessSample{{
			PID: 10, Name: "ollama runner", Executable: &executable, CPUPercent: 4.2,
			MemoryBytes: 30 << 30, StartTimeUnixSeconds: uint64(now.Add(-time.Hour).Unix()), Status: "Run",
		}},
	}
}
