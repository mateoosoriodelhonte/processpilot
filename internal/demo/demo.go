package demo

import (
	"context"
	"sort"
	"time"

	"github.com/mateoosoriodelhonte/processpilot/internal/analysis"
	"github.com/mateoosoriodelhonte/processpilot/internal/protocol"
)

const gib = uint64(1) << 30

// Snapshot returns deterministic example values for screenshots, documentation,
// and browser tests. The caller supplies time and sequence identity explicitly.
func Snapshot(at time.Time, sequence uint64) protocol.Snapshot {
	start := uint64(at.Add(-3 * time.Hour).Unix())
	pid1, pid301, pid410, pid510, pid610 := uint32(1), uint32(301), uint32(410), uint32(510), uint32(610)
	ollama := "/Applications/Ollama.app/Contents/MacOS/ollama"
	qemu := "/opt/homebrew/bin/qemu-system-aarch64"
	firefox := "/Applications/Firefox.app/Contents/MacOS/firefox"
	firefoxHelper := "/Applications/Firefox.app/Contents/MacOS/plugin-container"
	code := "/Applications/Visual Studio Code.app/Contents/MacOS/Electron"
	rust := "/opt/homebrew/bin/rustc"
	launchd := "/sbin/launchd"

	return protocol.Snapshot{
		ProtocolVersion: protocol.Version,
		TimestampUnixMS: uint64(at.UnixMilli()),
		Sequence:        sequence,
		System: protocol.SystemSample{
			TotalMemoryBytes: 64 * gib, UsedMemoryBytes: 55 * gib, AvailableMemoryBytes: 9 * gib,
			TotalSwapBytes: 8 * gib, UsedSwapBytes: 2 * gib, CPUPercent: 67.4,
			LoadAverage1: 12.4, LoadAverage5: 9.8, LoadAverage15: 7.2, LogicalCPUCount: 12,
		},
		Processes: []protocol.ProcessSample{
			{PID: pid1, Name: "launchd", Executable: &launchd, CPUPercent: 0.2, MemoryBytes: 90 << 20, StartTimeUnixSeconds: start, Status: "Run"},
			{PID: pid301, ParentPID: &pid1, Name: "ollama", Executable: &ollama, CPUPercent: 2.1, MemoryBytes: 410 << 20, StartTimeUnixSeconds: start, Status: "Sleep"},
			{PID: 95707, ParentPID: &pid301, Name: "ollama runner", Executable: &ollama, CPUPercent: 31.8, MemoryBytes: 30 * gib, StartTimeUnixSeconds: start, Status: "Run"},
			{PID: pid410, ParentPID: &pid1, Name: "qemu-system-aarch64", Executable: &qemu, CPUPercent: 18.5, MemoryBytes: 16 * gib, StartTimeUnixSeconds: start, Status: "Run"},
			{PID: pid510, ParentPID: &pid1, Name: "firefox", Executable: &firefox, CPUPercent: 4.7, MemoryBytes: 1 * gib, StartTimeUnixSeconds: start, Status: "Sleep"},
			{PID: 511, ParentPID: &pid510, Name: "firefox cp webcontent", Executable: &firefoxHelper, CPUPercent: 7.3, MemoryBytes: 4800 << 20, StartTimeUnixSeconds: start, Status: "Run"},
			{PID: pid610, ParentPID: &pid1, Name: "Code Helper", Executable: &code, CPUPercent: 1.8, MemoryBytes: 2400 << 20, StartTimeUnixSeconds: start, Status: "Sleep"},
			{PID: 611, ParentPID: &pid610, Name: "rustc", Executable: &rust, CPUPercent: 14.2, MemoryBytes: 1200 << 20, StartTimeUnixSeconds: start, Status: "Run"},
		},
	}
}

type Store struct {
	history []analysis.HistoryPoint
}

func NewStore(anchor time.Time) *Store {
	values := []struct {
		ago         time.Duration
		application string
		memory      uint64
		cpu         float64
	}{
		{50 * time.Minute, "Ollama", 12 * gib, 18}, {40 * time.Minute, "Ollama", 13 * gib, 22},
		{30 * time.Minute, "Ollama", 14 * gib, 85}, {20 * time.Minute, "Ollama", 15 * gib, 88},
		{50 * time.Minute, "Virtual machine", 15 * gib, 15}, {30 * time.Minute, "Virtual machine", 16 * gib, 17},
		{50 * time.Minute, "Firefox", 5 * gib, 6}, {30 * time.Minute, "Firefox", 5600 << 20, 9},
	}
	history := make([]analysis.HistoryPoint, 0, len(values))
	for _, value := range values {
		history = append(history, analysis.HistoryPoint{
			Timestamp: anchor.Add(-value.ago), Application: value.application,
			MemoryBytes: value.memory, CPUPercent: value.cpu,
		})
	}
	sort.Slice(history, func(i, j int) bool {
		if history[i].Timestamp.Equal(history[j].Timestamp) {
			return history[i].Application < history[j].Application
		}
		return history[i].Timestamp.Before(history[j].Timestamp)
	})
	return &Store{history: history}
}

func (*Store) Record(context.Context, protocol.Snapshot, analysis.Result) error { return nil }

func (*Store) Cleanup(context.Context, time.Time) (int64, error) { return 0, nil }

func (store *Store) AllApplicationHistory(_ context.Context, since time.Time, limit int) ([]analysis.HistoryPoint, error) {
	return store.filtered("", since, limit), nil
}

func (store *Store) ApplicationHistory(_ context.Context, application string, since time.Time, limit int) ([]analysis.HistoryPoint, error) {
	return store.filtered(application, since, limit), nil
}

func (store *Store) filtered(application string, since time.Time, limit int) []analysis.HistoryPoint {
	if limit <= 0 {
		return nil
	}
	result := make([]analysis.HistoryPoint, 0, min(limit, len(store.history)))
	for _, point := range store.history {
		if point.Timestamp.Before(since) || (application != "" && point.Application != application) {
			continue
		}
		result = append(result, point)
		if len(result) == limit {
			break
		}
	}
	return result
}
