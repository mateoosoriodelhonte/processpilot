// Command performance measures the real collector, analysis, state, anomaly, and SQLite path.
// It stores only in a newly created temporary directory and prints aggregate counts.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/mateoosoriodelhonte/processpilot/internal/ai"
	"github.com/mateoosoriodelhonte/processpilot/internal/analysis"
	"github.com/mateoosoriodelhonte/processpilot/internal/app"
	collectorclient "github.com/mateoosoriodelhonte/processpilot/internal/collector"
	"github.com/mateoosoriodelhonte/processpilot/internal/store"
)

const (
	logicalDuration = 30 * time.Minute
	sampleInterval  = 2 * time.Second
)

func main() {
	if err := measure(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func measure() error {
	collectorPath, err := filepath.Abs("target/release/processpilot-collector")
	if err != nil {
		return err
	}
	supervisor, err := collectorclient.NewSupervisor(collectorPath, sampleInterval)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	snapshot, err := supervisor.Sample(ctx)
	if err != nil {
		return fmt.Errorf("sample real collector: %w", err)
	}
	result := analysis.Build(snapshot.Processes)

	directory, err := os.MkdirTemp("", "processpilot-performance-")
	if err != nil {
		return fmt.Errorf("create measurement directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(directory) }()
	databasePath := filepath.Join(directory, "processpilot.db")
	telemetryStore, err := store.Open(databasePath, store.DefaultRetention)
	if err != nil {
		return err
	}
	service := app.NewService(telemetryStore, ai.NoAIProvider{}, false)

	sampleCount := int(logicalDuration / sampleInterval)
	base := time.Now().UTC().Add(-logicalDuration)
	started := time.Now()
	for index := range sampleCount {
		at := base.Add(time.Duration(index) * sampleInterval)
		snapshot.TimestampUnixMS = uint64(at.UnixMilli())
		snapshot.Sequence = uint64(index + 1)
		if err := service.Accept(context.Background(), snapshot); err != nil {
			_ = telemetryStore.Close()
			return fmt.Errorf("accept measurement sample %d: %w", index, err)
		}
	}
	writeDuration := time.Since(started)
	if err := telemetryStore.Close(); err != nil {
		return err
	}
	databaseBytes, err := relatedFileBytes(databasePath)
	if err != nil {
		return err
	}

	persistedSamples := int(logicalDuration / store.HistoryInterval)
	fmt.Printf("process_count=%d\napplication_count=%d\ncollector_samples=%d\npersisted_samples=%d\nlogical_duration=%s\ningestion_duration=%s\nsqlite_bytes=%d\nbytes_per_persisted_sample=%.1f\n",
		len(snapshot.Processes), len(result.Applications), sampleCount, persistedSamples, logicalDuration,
		writeDuration.Round(time.Millisecond), databaseBytes, float64(databaseBytes)/float64(persistedSamples))
	return nil
}

func relatedFileBytes(databasePath string) (int64, error) {
	var total int64
	for _, path := range []string{databasePath, databasePath + "-wal", databasePath + "-shm"} {
		info, err := os.Stat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return 0, err
		}
		total += info.Size()
	}
	return total, nil
}
