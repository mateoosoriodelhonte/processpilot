package analysis

import (
	"fmt"
	"sort"
	"time"
)

type AnomalyKind string

const (
	AnomalyMemorySpike      AnomalyKind = "MEMORY_SPIKE"
	AnomalySustainedCPU     AnomalyKind = "SUSTAINED_CPU"
	AnomalyMemoryGrowth     AnomalyKind = "MEMORY_GROWTH"
	AnomalyNewMajorConsumer AnomalyKind = "NEW_MAJOR_CONSUMER"
)

type HistoryPoint struct {
	Timestamp   time.Time `json:"timestamp"`
	Application string    `json:"application"`
	MemoryBytes uint64    `json:"memoryBytes"`
	CPUPercent  float64   `json:"cpuPercent"`
}

type Anomaly struct {
	Kind        AnomalyKind `json:"kind"`
	Application string      `json:"application"`
	DetectedAt  time.Time   `json:"detectedAt"`
	Rule        string      `json:"rule"`
	Evidence    string      `json:"evidence"`
}

func DetectAnomalies(history []HistoryPoint, current []Application, totalMemory uint64, now time.Time) []Anomaly {
	byApplication := make(map[string][]HistoryPoint)
	for _, point := range history {
		byApplication[point.Application] = append(byApplication[point.Application], point)
	}
	for application := range byApplication {
		sort.Slice(byApplication[application], func(i, j int) bool {
			return byApplication[application][i].Timestamp.Before(byApplication[application][j].Timestamp)
		})
	}

	var anomalies []Anomaly
	for _, application := range current {
		points := byApplication[application.Name]
		if len(points) == 0 && totalMemory > 0 && application.MemoryBytes >= 1<<30 && float64(application.MemoryBytes)/float64(totalMemory) >= 0.10 {
			anomalies = append(anomalies, Anomaly{
				Kind: AnomalyNewMajorConsumer, Application: application.Name, DetectedAt: now,
				Rule:     "An application absent from retained history uses at least 1 GiB and 10% of physical memory.",
				Evidence: fmt.Sprintf("Current memory is %.2f GiB (%.1f%% of physical RAM).", gibibytes(application.MemoryBytes), float64(application.MemoryBytes)/float64(totalMemory)*100),
			})
		}
		if len(points) >= 4 && memorySpike(points, application.MemoryBytes) {
			baseline := averageMemory(last(points, 10))
			anomalies = append(anomalies, Anomaly{
				Kind: AnomalyMemorySpike, Application: application.Name, DetectedAt: now,
				Rule:     "Current memory is at least 1.75× the recent average and at least 512 MiB above it.",
				Evidence: fmt.Sprintf("Current %.2f GiB; recent average %.2f GiB.", gibibytes(application.MemoryBytes), gibibytes(baseline)),
			})
		}
		if len(points) >= 2 && sustainedCPU(points, application.CPUPercent) {
			anomalies = append(anomalies, Anomaly{
				Kind: AnomalySustainedCPU, Application: application.Name, DetectedAt: now,
				Rule:     "CPU is at least 80% in the current sample and two consecutive retained samples.",
				Evidence: fmt.Sprintf("Latest three CPU samples are %.1f%%, %.1f%%, and %.1f%%.", points[len(points)-2].CPUPercent, points[len(points)-1].CPUPercent, application.CPUPercent),
			})
		}
		if len(points) >= 4 && memoryGrowth(points, application.MemoryBytes) {
			start := points[len(points)-4].MemoryBytes
			anomalies = append(anomalies, Anomaly{
				Kind: AnomalyMemoryGrowth, Application: application.Name, DetectedAt: now,
				Rule:     "Five consecutive samples do not decrease and memory grows by at least 25% and 256 MiB.",
				Evidence: fmt.Sprintf("Memory grew from %.2f GiB to %.2f GiB across five samples.", gibibytes(start), gibibytes(application.MemoryBytes)),
			})
		}
	}

	sort.Slice(anomalies, func(i, j int) bool {
		if anomalies[i].Application == anomalies[j].Application {
			return anomalies[i].Kind < anomalies[j].Kind
		}
		return anomalies[i].Application < anomalies[j].Application
	})
	return anomalies
}

func memorySpike(points []HistoryPoint, current uint64) bool {
	baseline := averageMemory(last(points, 10))
	return baseline > 0 && float64(current) >= float64(baseline)*1.75 && current >= baseline+512<<20
}

func sustainedCPU(points []HistoryPoint, current float64) bool {
	return current >= 80 && points[len(points)-1].CPUPercent >= 80 && points[len(points)-2].CPUPercent >= 80
}

func memoryGrowth(points []HistoryPoint, current uint64) bool {
	recent := last(points, 4)
	previous := recent[0].MemoryBytes
	for _, point := range recent[1:] {
		if point.MemoryBytes < previous {
			return false
		}
		previous = point.MemoryBytes
	}
	if current < previous {
		return false
	}
	start := recent[0].MemoryBytes
	return start > 0 && float64(current) >= float64(start)*1.25 && current >= start+256<<20
}

func averageMemory(points []HistoryPoint) uint64 {
	if len(points) == 0 {
		return 0
	}
	divisor := uint64(len(points))
	var quotientTotal uint64
	var remainderTotal uint64
	for _, point := range points {
		quotientTotal += point.MemoryBytes / divisor
		remainderTotal += point.MemoryBytes % divisor
	}
	return quotientTotal + remainderTotal/divisor
}

func last(points []HistoryPoint, count int) []HistoryPoint {
	if len(points) <= count {
		return points
	}
	return points[len(points)-count:]
}
