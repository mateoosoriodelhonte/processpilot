package app

import (
	"context"
	"errors"
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
		if update.Sequence != snapshot.Sequence {
			t.Fatalf("update sequence = %d", update.Sequence)
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

func TestAcceptCachesBoundedAnomalyHistoryBetweenRetentionSamples(t *testing.T) {
	store := &fakeStore{}
	service := NewService(store, ai.NoAIProvider{}, false)
	first := serviceSnapshot()
	second := first
	second.Sequence++
	second.TimestampUnixMS += uint64((2 * time.Second).Milliseconds())

	if err := service.Accept(context.Background(), first); err != nil {
		t.Fatalf("Accept(first) error = %v", err)
	}
	if err := service.Accept(context.Background(), second); err != nil {
		t.Fatalf("Accept(second) error = %v", err)
	}
	if store.anomalyReads != 1 {
		t.Fatalf("anomaly history reads = %d, want 1 per minute", store.anomalyReads)
	}
}

func TestAcceptKeepsCurrentTelemetryAvailableWhenHistoryFails(t *testing.T) {
	service := NewService(&fakeStore{failure: errors.New("database unavailable")}, ai.NoAIProvider{}, false)
	if err := service.Accept(context.Background(), serviceSnapshot()); err != nil {
		t.Fatalf("Accept() error = %v, want graceful degradation", err)
	}
	state, ok := service.Current()
	if !ok || state.Warning != persistenceWarning || len(state.Analysis.Processes) == 0 {
		t.Fatalf("degraded state = %#v", state)
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

func TestLiveUpdateIsBoundedAndOmitsPIDLists(t *testing.T) {
	applications := make([]analysis.Application, maximumUpdateApplications+1)
	for index := range applications {
		applications[index] = analysis.Application{Name: "application", PIDs: []uint32{uint32(index + 1)}}
	}
	update := stateUpdate(State{Analysis: analysis.Result{Applications: applications}})
	if len(update.Applications) != maximumUpdateApplications {
		t.Fatalf("update applications = %d, want %d", len(update.Applications), maximumUpdateApplications)
	}
	for _, application := range update.Applications {
		if application.PIDs != nil {
			t.Fatalf("live update retained PID list: %#v", application.PIDs)
		}
	}
}

func TestPartialSnapshotSuppressesAnomalies(t *testing.T) {
	snapshot := serviceSnapshot()
	snapshot.ProcessesTruncated = true
	service := NewService(&fakeStore{history: []analysis.HistoryPoint{{
		Timestamp: time.UnixMilli(int64(snapshot.TimestampUnixMS)).Add(-time.Minute),
		Key:       "known:Ollama", Application: "Ollama", CPUPercent: 100, MemoryBytes: 1 << 30,
	}}}, ai.NoAIProvider{}, false)
	if err := service.Accept(context.Background(), snapshot); err != nil {
		t.Fatalf("Accept() error = %v", err)
	}
	state, _ := service.Current()
	if len(state.Anomalies) != 0 {
		t.Fatalf("partial snapshot produced anomalies: %#v", state.Anomalies)
	}
}

type fakeStore struct {
	records      int
	cleanups     int
	anomalyReads int
	history      []analysis.HistoryPoint
	failure      error
}

func (store *fakeStore) Record(context.Context, protocol.Snapshot, analysis.Result) error {
	store.records++
	return store.failure
}

func (store *fakeStore) Cleanup(context.Context, time.Time) (int64, error) {
	store.cleanups++
	return 0, store.failure
}

func (store *fakeStore) AllApplicationHistory(context.Context, time.Time, int) ([]analysis.HistoryPoint, error) {
	return store.history, nil
}

func (store *fakeStore) AnomalyHistory(context.Context, time.Time, int, []string) ([]analysis.HistoryPoint, error) {
	store.anomalyReads++
	return store.history, store.failure
}

func (store *fakeStore) ApplicationHistory(context.Context, string, time.Time, int) ([]analysis.HistoryPoint, error) {
	return store.history, nil
}

func (store *fakeStore) ApplicationKeyHistory(context.Context, string, time.Time, int) ([]analysis.HistoryPoint, error) {
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
