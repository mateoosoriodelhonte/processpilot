package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
)

const (
	Version        = 1
	MaxLineBytes   = 4 << 20
	maxProcesses   = 100_000
	maxNameBytes   = 256
	maxPathBytes   = 2_048
	maxStatusBytes = 64
)

var (
	ErrMalformed          = errors.New("malformed collector snapshot")
	ErrUnsupportedVersion = errors.New("unsupported collector protocol version")
)

type Snapshot struct {
	ProtocolVersion uint32          `json:"protocolVersion"`
	TimestampUnixMS uint64          `json:"timestampUnixMs"`
	Sequence        uint64          `json:"sequence"`
	System          SystemSample    `json:"system"`
	Processes       []ProcessSample `json:"processes"`
}

type SystemSample struct {
	TotalMemoryBytes     uint64  `json:"totalMemoryBytes"`
	UsedMemoryBytes      uint64  `json:"usedMemoryBytes"`
	AvailableMemoryBytes uint64  `json:"availableMemoryBytes"`
	TotalSwapBytes       uint64  `json:"totalSwapBytes"`
	UsedSwapBytes        uint64  `json:"usedSwapBytes"`
	CPUPercent           float64 `json:"cpuPercent"`
	LoadAverage1         float64 `json:"loadAverage1"`
	LoadAverage5         float64 `json:"loadAverage5"`
	LoadAverage15        float64 `json:"loadAverage15"`
	LogicalCPUCount      int     `json:"logicalCpuCount"`
}

type ProcessSample struct {
	PID                  uint32  `json:"pid"`
	ParentPID            *uint32 `json:"parentPid"`
	Name                 string  `json:"name"`
	Executable           *string `json:"executable"`
	CPUPercent           float64 `json:"cpuPercent"`
	MemoryBytes          uint64  `json:"memoryBytes"`
	StartTimeUnixSeconds uint64  `json:"startTimeUnixSeconds"`
	Status               string  `json:"status"`
}

func Decode(raw []byte) (Snapshot, error) {
	if len(raw) == 0 || len(raw) > MaxLineBytes {
		return Snapshot{}, fmt.Errorf("%w: line length %d is outside 1..%d", ErrMalformed, len(raw), MaxLineBytes)
	}

	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var snapshot Snapshot
	if err := decoder.Decode(&snapshot); err != nil {
		return Snapshot{}, fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	if err := requireEOF(decoder); err != nil {
		return Snapshot{}, err
	}
	if snapshot.ProtocolVersion != Version {
		return Snapshot{}, fmt.Errorf("%w: got %d, want %d", ErrUnsupportedVersion, snapshot.ProtocolVersion, Version)
	}
	if err := snapshot.validate(); err != nil {
		return Snapshot{}, err
	}
	return snapshot, nil
}

func requireEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("%w: trailing JSON value", ErrMalformed)
		}
		return fmt.Errorf("%w: trailing data: %v", ErrMalformed, err)
	}
	return nil
}

func (s Snapshot) validate() error {
	if s.TimestampUnixMS == 0 {
		return invalid("timestampUnixMs must be positive")
	}
	if len(s.Processes) > maxProcesses {
		return invalid("process count %d exceeds %d", len(s.Processes), maxProcesses)
	}
	if s.System.LogicalCPUCount < 1 || s.System.LogicalCPUCount > 1_024 {
		return invalid("logicalCpuCount is outside 1..1024")
	}
	if s.System.UsedMemoryBytes > s.System.TotalMemoryBytes || s.System.AvailableMemoryBytes > s.System.TotalMemoryBytes {
		return invalid("system memory values exceed total memory")
	}
	if s.System.UsedSwapBytes > s.System.TotalSwapBytes {
		return invalid("used swap exceeds total swap")
	}
	if !inRange(s.System.CPUPercent, 0, 100) || !nonNegativeFinite(s.System.LoadAverage1) || !nonNegativeFinite(s.System.LoadAverage5) || !nonNegativeFinite(s.System.LoadAverage15) {
		return invalid("system CPU or load value is invalid")
	}

	seen := make(map[uint32]struct{}, len(s.Processes))
	for i, process := range s.Processes {
		if process.PID == 0 {
			return invalid("processes[%d].pid must be positive", i)
		}
		if _, exists := seen[process.PID]; exists {
			return invalid("duplicate pid %d", process.PID)
		}
		seen[process.PID] = struct{}{}
		if process.ParentPID != nil && *process.ParentPID == process.PID {
			return invalid("processes[%d] is its own parent", i)
		}
		if strings.TrimSpace(process.Name) == "" || len(process.Name) > maxNameBytes {
			return invalid("processes[%d].name is empty or too long", i)
		}
		if process.Executable != nil && len(*process.Executable) > maxPathBytes {
			return invalid("processes[%d].executable is too long", i)
		}
		maxCPU := float64(s.System.LogicalCPUCount) * 100
		if !inRange(process.CPUPercent, 0, maxCPU) {
			return invalid("processes[%d].cpuPercent is invalid", i)
		}
		if process.MemoryBytes > s.System.TotalMemoryBytes {
			return invalid("processes[%d].memoryBytes exceeds physical memory", i)
		}
		if process.StartTimeUnixSeconds == 0 || process.StartTimeUnixSeconds > s.TimestampUnixMS/1_000+300 {
			return invalid("processes[%d].startTimeUnixSeconds is invalid", i)
		}
		if strings.TrimSpace(process.Status) == "" || len(process.Status) > maxStatusBytes {
			return invalid("processes[%d].status is empty or too long", i)
		}
	}
	return nil
}

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrMalformed, fmt.Sprintf(format, args...))
}

func inRange(value, minimum, maximum float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= minimum && value <= maximum
}

func nonNegativeFinite(value float64) bool {
	return inRange(value, 0, math.MaxFloat64)
}
