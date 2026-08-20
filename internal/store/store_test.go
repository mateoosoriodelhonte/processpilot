package store

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mateoosoriodelhonte/processpilot/internal/analysis"
	"github.com/mateoosoriodelhonte/processpilot/internal/protocol"
)

func TestOpenMigratesToAPrivacyBoundedSchema(t *testing.T) {
	store := openTestStore(t, 7*24*time.Hour)

	for _, table := range []string{"system_samples", "application_samples"} {
		rows, err := store.db.QueryContext(context.Background(), "PRAGMA table_info("+table+")")
		if err != nil {
			t.Fatalf("table info %s: %v", table, err)
		}
		for rows.Next() {
			var cid int
			var name, kind string
			var notNull, primaryKey int
			var defaultValue any
			if err := rows.Scan(&cid, &name, &kind, &notNull, &defaultValue, &primaryKey); err != nil {
				t.Fatalf("scan table info: %v", err)
			}
			lower := strings.ToLower(name)
			for _, prohibited := range []string{"command", "environment", "working_directory", "file_content"} {
				if strings.Contains(lower, prohibited) {
					t.Fatalf("table %s has prohibited column %q", table, name)
				}
			}
		}
		if err := rows.Close(); err != nil {
			t.Fatalf("close table info: %v", err)
		}
	}
	var processTableCount int
	if err := store.db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'process_samples'").Scan(&processTableCount); err != nil {
		t.Fatalf("inspect process persistence: %v", err)
	}
	if processTableCount != 0 {
		t.Fatal("raw process snapshots must remain memory-only")
	}
}

func TestRecordAndHistoryRoundTripSanitizedTelemetry(t *testing.T) {
	store := openTestStore(t, 7*24*time.Hour)
	snapshot := testSnapshot(time.Unix(1_787_250_000, 0), "Ollama")
	result := analysis.Build(snapshot.Processes)

	if err := store.Record(context.Background(), snapshot, result); err != nil {
		t.Fatalf("Record() error = %v", err)
	}
	history, err := store.ApplicationHistory(context.Background(), "Ollama", time.Unix(0, 0), 100)
	if err != nil {
		t.Fatalf("ApplicationHistory() error = %v", err)
	}
	if len(history) != 1 || history[0].Application != "Ollama" || history[0].MemoryBytes != 10<<30 {
		t.Fatalf("history = %#v", history)
	}
}

func TestRecordKeepsSameNamedUnknownProcessesSeparate(t *testing.T) {
	telemetryStore := openTestStore(t, 7*24*time.Hour)
	now := time.Unix(1_787_250_000, 0)
	snapshot := testSnapshot(now, "worker")
	snapshot.Processes = []protocol.ProcessSample{
		{PID: 10, Name: "worker", CPUPercent: 1, MemoryBytes: 1 << 20, StartTimeUnixSeconds: uint64(now.Add(-time.Hour).Unix()), Status: "Run"},
		{PID: 11, Name: "worker", CPUPercent: 2, MemoryBytes: 2 << 20, StartTimeUnixSeconds: uint64(now.Add(-time.Hour).Unix()), Status: "Run"},
	}
	result := analysis.Build(snapshot.Processes)
	if len(result.Applications) != 2 {
		t.Fatalf("applications = %#v", result.Applications)
	}
	if err := telemetryStore.Record(context.Background(), snapshot, result); err != nil {
		t.Fatalf("Record() error = %v", err)
	}
	var count int
	if err := telemetryStore.db.QueryRow("SELECT COUNT(*) FROM application_samples").Scan(&count); err != nil {
		t.Fatalf("count application samples: %v", err)
	}
	if count != 2 {
		t.Fatalf("persisted application count = %d", count)
	}
}

func TestRecordDownsamplesHistoryAndBoundsApplicationRows(t *testing.T) {
	telemetryStore := openTestStore(t, 7*24*time.Hour)
	base := time.Unix(1_787_250_000, 0)
	for _, offset := range []time.Duration{0, 2 * time.Second, 59 * time.Second, time.Minute} {
		snapshot := testSnapshot(base.Add(offset), "Ollama")
		if err := telemetryStore.Record(context.Background(), snapshot, analysis.Build(snapshot.Processes)); err != nil {
			t.Fatalf("Record(%s) error = %v", offset, err)
		}
	}
	var systemCount int
	if err := telemetryStore.db.QueryRow("SELECT COUNT(*) FROM system_samples").Scan(&systemCount); err != nil {
		t.Fatalf("count system samples: %v", err)
	}
	if systemCount != 2 {
		t.Fatalf("persisted system samples = %d, want 2", systemCount)
	}

	now := base.Add(time.Hour)
	large := testSnapshot(now, "unused")
	large.Processes = make([]protocol.ProcessSample, MaximumHistoricalApplications+20)
	for index := range large.Processes {
		large.Processes[index] = protocol.ProcessSample{
			PID: uint32(index + 10), Name: "unknown-" + strconv.Itoa(index), CPUPercent: 1,
			MemoryBytes: uint64(index+1) << 20, StartTimeUnixSeconds: uint64(now.Add(-time.Hour).Unix()), Status: "Run",
		}
	}
	if err := telemetryStore.Record(context.Background(), large, analysis.Build(large.Processes)); err != nil {
		t.Fatalf("Record(large) error = %v", err)
	}
	var latestApplicationCount int
	if err := telemetryStore.db.QueryRow("SELECT COUNT(*) FROM application_samples WHERE timestamp_ms = ?", large.TimestampUnixMS).Scan(&latestApplicationCount); err != nil {
		t.Fatalf("count bounded applications: %v", err)
	}
	if latestApplicationCount != MaximumHistoricalApplications {
		t.Fatalf("persisted applications = %d, want %d", latestApplicationCount, MaximumHistoricalApplications)
	}
}

func TestApplicationHistoryUsesParametersNotSQLConcatenation(t *testing.T) {
	store := openTestStore(t, 7*24*time.Hour)
	snapshot := testSnapshot(time.Unix(1_787_250_000, 0), "normal")
	if err := store.Record(context.Background(), snapshot, analysis.Build(snapshot.Processes)); err != nil {
		t.Fatalf("Record() error = %v", err)
	}

	history, err := store.ApplicationHistory(context.Background(), `normal' OR 1=1 --`, time.Unix(0, 0), 100)

	if err != nil {
		t.Fatalf("ApplicationHistory() error = %v", err)
	}
	if len(history) != 0 {
		t.Fatalf("injection-like application matched rows: %#v", history)
	}
}

func TestHistoryLimitReturnsNewestEvidenceInChronologicalOrder(t *testing.T) {
	telemetryStore := openTestStore(t, 7*24*time.Hour)
	base := time.Unix(1_787_250_000, 0)
	for index := range 3 {
		snapshot := testSnapshot(base.Add(time.Duration(index)*time.Minute), "Ollama")
		if err := telemetryStore.Record(context.Background(), snapshot, analysis.Build(snapshot.Processes)); err != nil {
			t.Fatalf("Record() error = %v", err)
		}
	}

	history, err := telemetryStore.ApplicationHistory(context.Background(), "Ollama", time.Unix(0, 0), 2)
	if err != nil {
		t.Fatalf("ApplicationHistory() error = %v", err)
	}
	if len(history) != 2 || !history[0].Timestamp.Equal(base.Add(time.Minute)) || !history[1].Timestamp.Equal(base.Add(2*time.Minute)) {
		t.Fatalf("history = %#v, want latest two in chronological order", history)
	}
}

func TestCleanupEnforcesConfiguredRetention(t *testing.T) {
	store := openTestStore(t, 24*time.Hour)
	old := testSnapshot(time.Unix(1_700_000_000, 0), "Old")
	recentTime := time.Unix(1_700_086_400, 0)
	recent := testSnapshot(recentTime, "Recent")
	for _, snapshot := range []protocol.Snapshot{old, recent} {
		if err := store.Record(context.Background(), snapshot, analysis.Build(snapshot.Processes)); err != nil {
			t.Fatalf("Record() error = %v", err)
		}
	}

	deleted, err := store.Cleanup(context.Background(), recentTime.Add(time.Hour))

	if err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if deleted == 0 {
		t.Fatal("Cleanup() deleted no rows")
	}
	history, err := store.ApplicationHistory(context.Background(), "Old", time.Unix(0, 0), 100)
	if err != nil {
		t.Fatalf("ApplicationHistory() error = %v", err)
	}
	if len(history) != 0 {
		t.Fatalf("expired history remains: %#v", history)
	}
}

func TestRetentionValidationFailsClosed(t *testing.T) {
	for _, retention := range []time.Duration{0, 59 * time.Minute, 31 * 24 * time.Hour} {
		path := filepath.Join(t.TempDir(), "processpilot.db")
		if _, err := Open(path, retention); err == nil {
			t.Fatalf("Open(retention=%s) error = nil", retention)
		}
	}
}

func openTestStore(t *testing.T, retention time.Duration) *Store {
	t.Helper()
	store, err := Open(filepath.Join(t.TempDir(), "processpilot.db"), retention)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func testSnapshot(timestamp time.Time, application string) protocol.Snapshot {
	executable := "/Applications/" + application + ".app/Contents/MacOS/" + application
	return protocol.Snapshot{
		ProtocolVersion: protocol.Version,
		TimestampUnixMS: uint64(timestamp.UnixMilli()),
		Sequence:        uint64(timestamp.Unix()),
		System: protocol.SystemSample{
			TotalMemoryBytes: 64 << 30, UsedMemoryBytes: 20 << 30, AvailableMemoryBytes: 44 << 30,
			LogicalCPUCount: 8, CPUPercent: 20,
		},
		Processes: []protocol.ProcessSample{{
			PID: 10, Name: application, Executable: &executable, CPUPercent: 5,
			MemoryBytes: 10 << 30, StartTimeUnixSeconds: uint64(timestamp.Add(-time.Hour).Unix()), Status: "Run",
		}},
	}
}
