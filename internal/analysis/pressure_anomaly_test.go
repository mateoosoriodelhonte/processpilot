package analysis

import (
	"testing"
	"time"

	"github.com/mateoosoriodelhonte/processpilot/internal/protocol"
)

func TestAssessPressureUsesDocumentedBoundaries(t *testing.T) {
	base := protocol.SystemSample{
		TotalMemoryBytes: 100, UsedMemoryBytes: 50, AvailableMemoryBytes: 50,
		TotalSwapBytes: 10 << 30, LogicalCPUCount: 8,
	}
	tests := []struct {
		name   string
		mutate func(*protocol.SystemSample)
		want   PressureLevel
	}{
		{name: "normal", mutate: func(*protocol.SystemSample) {}, want: PressureNormal},
		{name: "elevated memory", mutate: func(system *protocol.SystemSample) { system.UsedMemoryBytes = 75 }, want: PressureElevated},
		{name: "high memory", mutate: func(system *protocol.SystemSample) { system.UsedMemoryBytes = 90 }, want: PressureHigh},
		{name: "elevated CPU", mutate: func(system *protocol.SystemSample) { system.CPUPercent = 70 }, want: PressureElevated},
		{name: "high swap", mutate: func(system *protocol.SystemSample) { system.UsedSwapBytes = 4 << 30 }, want: PressureHigh},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			system := base
			tt.mutate(&system)
			pressure := AssessPressure(system)
			if pressure.Level != tt.want {
				t.Fatalf("Level = %s, want %s (%#v)", pressure.Level, tt.want, pressure)
			}
			if pressure.Summary == "" || len(pressure.Evidence) == 0 {
				t.Fatalf("pressure lacks explanation: %#v", pressure)
			}
		})
	}
}

func TestAssessPressureIsUnknownWithoutPhysicalMemory(t *testing.T) {
	pressure := AssessPressure(protocol.SystemSample{LogicalCPUCount: 8})

	if pressure.Level != PressureUnknown {
		t.Fatalf("Level = %s, want UNKNOWN", pressure.Level)
	}
}

func TestDetectAnomaliesExplainsMemorySpikeSustainedCPUAndGrowth(t *testing.T) {
	baseTime := time.Unix(1_787_250_000, 0)
	var history []HistoryPoint
	for index := 0; index < 5; index++ {
		history = append(history, HistoryPoint{
			Timestamp:   baseTime.Add(time.Duration(index) * time.Minute),
			Application: "Ollama", MemoryBytes: uint64(1+index) << 30, CPUPercent: 85,
		})
	}
	current := []Application{{Name: "Ollama", MemoryBytes: 10 << 30, CPUPercent: 90}}

	anomalies := DetectAnomalies(history, current, 64<<30, baseTime.Add(6*time.Minute))

	assertAnomalyKind(t, anomalies, AnomalyMemorySpike)
	assertAnomalyKind(t, anomalies, AnomalySustainedCPU)
	assertAnomalyKind(t, anomalies, AnomalyMemoryGrowth)
	for _, anomaly := range anomalies {
		if anomaly.Rule == "" || anomaly.Evidence == "" {
			t.Fatalf("anomaly lacks rule/evidence: %#v", anomaly)
		}
	}
}

func TestDetectAnomaliesFindsNewMajorConsumerButAvoidsClaimsWithoutEnoughHistory(t *testing.T) {
	now := time.Unix(1_787_250_000, 0)
	current := []Application{{Name: "New VM", MemoryBytes: 8 << 30, CPUPercent: 2}}

	history := []HistoryPoint{{Timestamp: now.Add(-time.Minute), Key: "known:Existing", Application: "Existing"}}
	anomalies := DetectAnomalies(history, current, 64<<30, now)
	assertAnomalyKind(t, anomalies, AnomalyNewMajorConsumer)
	if len(anomalies) != 1 {
		t.Fatalf("anomalies = %#v, want only new major consumer", anomalies)
	}

	small := []Application{{Name: "Tiny", MemoryBytes: 100 << 20, CPUPercent: 1}}
	if got := DetectAnomalies(nil, small, 64<<30, now); len(got) != 0 {
		t.Fatalf("insufficient history produced anomaly: %#v", got)
	}
}

func TestDetectAnomaliesRequiresRecentConsecutiveSamples(t *testing.T) {
	now := time.Unix(1_787_250_000, 0)
	history := []HistoryPoint{
		{Timestamp: now.Add(-48 * time.Hour), Key: "known:Ollama", Application: "Ollama", MemoryBytes: 1 << 30, CPUPercent: 90},
		{Timestamp: now.Add(-24 * time.Hour), Key: "known:Ollama", Application: "Ollama", MemoryBytes: 2 << 30, CPUPercent: 90},
		{Timestamp: now.Add(-12 * time.Hour), Key: "known:Ollama", Application: "Ollama", MemoryBytes: 3 << 30, CPUPercent: 90},
		{Timestamp: now.Add(-6 * time.Hour), Key: "known:Ollama", Application: "Ollama", MemoryBytes: 4 << 30, CPUPercent: 90},
	}
	current := []Application{{Key: "known:Ollama", Name: "Ollama", MemoryBytes: 10 << 30, CPUPercent: 90}}

	if anomalies := DetectAnomalies(history, current, 64<<30, now); len(anomalies) != 0 {
		t.Fatalf("stale, non-consecutive evidence produced anomalies: %#v", anomalies)
	}
}

func assertAnomalyKind(t *testing.T, anomalies []Anomaly, want AnomalyKind) {
	t.Helper()
	for _, anomaly := range anomalies {
		if anomaly.Kind == want {
			return
		}
	}
	t.Fatalf("anomalies %#v do not contain %s", anomalies, want)
}
