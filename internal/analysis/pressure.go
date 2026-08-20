package analysis

import (
	"fmt"

	"github.com/mateoosoriodelhonte/processpilot/internal/protocol"
)

type PressureLevel string

const (
	PressureNormal   PressureLevel = "NORMAL"
	PressureElevated PressureLevel = "ELEVATED"
	PressureHigh     PressureLevel = "HIGH"
	PressureUnknown  PressureLevel = "UNKNOWN"
)

type Pressure struct {
	Level    PressureLevel `json:"level"`
	Summary  string        `json:"summary"`
	Evidence []string      `json:"evidence"`
}

func AssessPressure(system protocol.SystemSample) Pressure {
	if system.TotalMemoryBytes == 0 || system.LogicalCPUCount <= 0 {
		return Pressure{
			Level: PressureUnknown, Summary: "Resource pressure is unavailable because required system metrics are missing.",
			Evidence: []string{"Physical memory total or logical CPU count was not available."},
		}
	}

	memoryPercent := float64(system.UsedMemoryBytes) / float64(system.TotalMemoryBytes) * 100
	loadPerCPU := system.LoadAverage1 / float64(system.LogicalCPUCount)
	evidence := []string{
		fmt.Sprintf("Memory usage is %.1f%% of physical RAM.", memoryPercent),
		fmt.Sprintf("System CPU usage is %.1f%%.", system.CPUPercent),
		fmt.Sprintf("Swap in use is %.2f GiB.", gibibytes(system.UsedSwapBytes)),
		fmt.Sprintf("One-minute load is %.2f per logical CPU.", loadPerCPU),
	}

	if memoryPercent >= 90 || system.CPUPercent >= 90 || system.UsedSwapBytes >= 4<<30 || loadPerCPU >= 2 {
		return Pressure{Level: PressureHigh, Summary: "At least one resource is under high pressure.", Evidence: evidence}
	}
	if memoryPercent >= 75 || system.CPUPercent >= 70 || system.UsedSwapBytes >= 1<<30 || loadPerCPU >= 1 {
		return Pressure{Level: PressureElevated, Summary: "Resource use is elevated and may affect responsiveness.", Evidence: evidence}
	}
	return Pressure{Level: PressureNormal, Summary: "Measured CPU, memory, swap, and load are within normal thresholds.", Evidence: evidence}
}

func gibibytes(bytes uint64) float64 {
	return float64(bytes) / float64(uint64(1)<<30)
}
