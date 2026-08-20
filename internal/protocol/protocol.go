package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
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
	ProtocolVersion    uint32          `json:"protocolVersion"`
	TimestampUnixMS    uint64          `json:"timestampUnixMs"`
	Sequence           uint64          `json:"sequence"`
	System             SystemSample    `json:"system"`
	ProcessesTruncated bool            `json:"processesTruncated"`
	Processes          []ProcessSample `json:"processes"`
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
	if !utf8.Valid(raw) {
		return Snapshot{}, fmt.Errorf("%w: collector output must be valid UTF-8", ErrMalformed)
	}
	if err := rejectDuplicateObjectKeys(raw); err != nil {
		return Snapshot{}, err
	}
	var header struct {
		ProtocolVersion *uint32 `json:"protocolVersion"`
	}
	if err := json.Unmarshal(raw, &header); err != nil {
		return Snapshot{}, fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	if header.ProtocolVersion == nil {
		return Snapshot{}, invalid("protocolVersion must be present and non-null")
	}
	if *header.ProtocolVersion != Version {
		return Snapshot{}, fmt.Errorf("%w: got %d, want %d", ErrUnsupportedVersion, *header.ProtocolVersion, Version)
	}
	if err := requireSnapshotFields(raw); err != nil {
		return Snapshot{}, err
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

func requireSnapshotFields(raw []byte) error {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err != nil {
		return fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	if err := requireFields(root, false, "protocolVersion", "timestampUnixMs", "sequence", "system", "processesTruncated", "processes"); err != nil {
		return err
	}

	var system map[string]json.RawMessage
	if err := json.Unmarshal(root["system"], &system); err != nil {
		return fmt.Errorf("%w: system must be an object", ErrMalformed)
	}
	if err := requireFields(system, false, "totalMemoryBytes", "usedMemoryBytes", "availableMemoryBytes", "totalSwapBytes", "usedSwapBytes", "cpuPercent", "loadAverage1", "loadAverage5", "loadAverage15", "logicalCpuCount"); err != nil {
		return err
	}

	var processes []json.RawMessage
	if err := json.Unmarshal(root["processes"], &processes); err != nil {
		return fmt.Errorf("%w: processes must be an array", ErrMalformed)
	}
	for index, rawProcess := range processes {
		var process map[string]json.RawMessage
		if err := json.Unmarshal(rawProcess, &process); err != nil {
			return fmt.Errorf("%w: processes[%d] must be an object", ErrMalformed, index)
		}
		if err := requireFields(process, true, "pid", "parentPid", "name", "executable", "cpuPercent", "memoryBytes", "startTimeUnixSeconds", "status"); err != nil {
			return fmt.Errorf("%w: processes[%d]: %v", ErrMalformed, index, err)
		}
	}
	return nil
}

func requireFields(object map[string]json.RawMessage, allowNullableIdentityFields bool, names ...string) error {
	for _, name := range names {
		value, exists := object[name]
		if !exists {
			return fmt.Errorf("%w: required field %s is missing", ErrMalformed, name)
		}
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) && !(allowNullableIdentityFields && (name == "parentPid" || name == "executable")) {
			return fmt.Errorf("%w: required field %s cannot be null", ErrMalformed, name)
		}
	}
	return nil
}

type jsonContainer struct {
	delimiter    json.Delim
	keys         map[string]struct{}
	expectingKey bool
}

func rejectDuplicateObjectKeys(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	containers := make([]jsonContainer, 0, 4)

	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("%w: %v", ErrMalformed, err)
		}

		switch value := token.(type) {
		case json.Delim:
			switch value {
			case '{', '[':
				markJSONValueConsumed(containers)
				container := jsonContainer{delimiter: value}
				if value == '{' {
					container.keys = make(map[string]struct{})
					container.expectingKey = true
				}
				containers = append(containers, container)
			case '}', ']':
				if len(containers) > 0 {
					containers = containers[:len(containers)-1]
				}
			}
		case string:
			if len(containers) > 0 {
				container := &containers[len(containers)-1]
				if container.delimiter == '{' && container.expectingKey {
					if _, exists := container.keys[value]; exists {
						return fmt.Errorf("%w: duplicate object key", ErrMalformed)
					}
					container.keys[value] = struct{}{}
					container.expectingKey = false
					continue
				}
			}
			markJSONValueConsumed(containers)
		default:
			markJSONValueConsumed(containers)
		}
	}
}

func markJSONValueConsumed(containers []jsonContainer) {
	if len(containers) == 0 {
		return
	}
	container := &containers[len(containers)-1]
	if container.delimiter == '{' && !container.expectingKey {
		container.expectingKey = true
	}
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
	maximumTimestamp := time.Now().UTC().Add(5 * time.Minute).UnixMilli()
	if s.TimestampUnixMS > uint64(maximumTimestamp) {
		return invalid("timestampUnixMs is implausibly far in the future")
	}
	if s.Sequence == 0 {
		return invalid("sequence must be positive")
	}
	if s.Processes == nil {
		return invalid("processes must be present as an array")
	}
	if exceedsSQLiteRange(
		s.TimestampUnixMS,
		s.Sequence,
		s.System.TotalMemoryBytes,
		s.System.UsedMemoryBytes,
		s.System.AvailableMemoryBytes,
		s.System.TotalSwapBytes,
		s.System.UsedSwapBytes,
	) {
		return invalid("unsigned system value exceeds SQLite integer range")
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
	var aggregateMemory uint64
	for i, process := range s.Processes {
		if process.PID == 0 {
			return invalid("processes[%d].pid must be positive", i)
		}
		if _, exists := seen[process.PID]; exists {
			return invalid("duplicate pid %d", process.PID)
		}
		seen[process.PID] = struct{}{}
		if process.ParentPID != nil && (*process.ParentPID == 0 || *process.ParentPID == process.PID) {
			return invalid("processes[%d].parentPid must be positive and different from pid", i)
		}
		if strings.TrimSpace(process.Name) == "" || len(process.Name) > maxNameBytes || containsControl(process.Name) || containsSecretAssignment(process.Name) {
			return invalid("processes[%d].name is empty or too long", i)
		}
		if process.Executable != nil {
			if reason := invalidExecutableIdentityReason(*process.Executable); reason != "" {
				return invalid("processes[%d].executable %s", i, reason)
			}
		}
		maxCPU := float64(s.System.LogicalCPUCount) * 100
		if !inRange(process.CPUPercent, 0, maxCPU) {
			return invalid("processes[%d].cpuPercent is invalid", i)
		}
		if process.MemoryBytes > s.System.TotalMemoryBytes {
			return invalid("processes[%d].memoryBytes exceeds physical memory", i)
		}
		if process.MemoryBytes > math.MaxInt64-aggregateMemory {
			return invalid("aggregate process memory exceeds the supported range")
		}
		aggregateMemory += process.MemoryBytes
		if process.StartTimeUnixSeconds == 0 || process.StartTimeUnixSeconds > s.TimestampUnixMS/1_000+300 {
			return invalid("processes[%d].startTimeUnixSeconds is invalid", i)
		}
		if exceedsSQLiteRange(process.MemoryBytes, process.StartTimeUnixSeconds) {
			return invalid("processes[%d] value exceeds SQLite integer range", i)
		}
		if strings.TrimSpace(process.Status) == "" || len(process.Status) > maxStatusBytes || containsControl(process.Status) {
			return invalid("processes[%d].status is empty or too long", i)
		}
	}
	return nil
}

func invalidExecutableIdentityReason(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "is empty"
	}
	if len(value) > maxPathBytes {
		return "is too long"
	}
	if containsControl(value) {
		return "contains control characters"
	}
	if containsSecretAssignment(value) {
		return "contains unredacted secret-like text"
	}
	lower := strings.ToLower(value)
	if strings.Contains(lower, "/users/") || strings.Contains(lower, "/system/volumes/data/users/") {
		return "contains an unredacted home path"
	}
	segments := strings.Split(value, "/")
	for index, segment := range segments {
		if segment != strings.TrimSpace(segment) || segment == "." || segment == ".." || (segment == "" && index != 0) {
			return "is not a canonical sanitized identity"
		}
	}
	if !isAllowedExecutableIdentity(value) {
		return "is outside the sanitized identity allowlist"
	}
	return ""
}

func isAllowedExecutableIdentity(value string) bool {
	if strings.HasPrefix(value, "/<private>/") {
		identity := strings.TrimPrefix(value, "/<private>/")
		return identity != "" && !strings.Contains(identity, "/")
	}
	if strings.HasPrefix(value, "~/<private>/") {
		identity := strings.TrimPrefix(value, "~/<private>/")
		return identity != "" && !strings.Contains(identity, "/")
	}
	if strings.HasPrefix(value, "~/Applications/") {
		return len(value) > len("~/Applications/")
	}
	for _, root := range []string{"/Applications/", "/System/", "/Library/", "/usr/", "/bin/", "/sbin/", "/opt/homebrew/"} {
		if strings.HasPrefix(value, root) && len(value) > len(root) {
			return true
		}
	}
	return false
}

func containsControl(value string) bool {
	return strings.IndexFunc(value, unicode.IsControl) >= 0
}

func containsSecretAssignment(value string) bool {
	lower := strings.ToLower(value)
	for _, field := range strings.Fields(lower) {
		if strings.Contains(field, "authorization:bearer") {
			return true
		}
		if scheme := strings.Index(field, "://"); scheme >= 0 {
			remainder := field[scheme+3:]
			if at := strings.IndexByte(remainder, '@'); at >= 0 && remainder[:at] != "[redacted]" {
				return true
			}
		}
	}
	for _, marker := range []string{"authorization=", "--token=", "--password=", "--secret=", "--api-key=", "api_key=", "token=", "password=", "secret="} {
		remaining := lower
		for {
			index := strings.Index(remaining, marker)
			if index < 0 {
				break
			}
			after := remaining[index+len(marker):]
			valueEnd := strings.IndexAny(after, " \t\r\n&;,")
			if valueEnd < 0 {
				valueEnd = len(after)
			}
			if after[:valueEnd] != "[redacted]" {
				return true
			}
			remaining = after[valueEnd:]
		}
	}
	fields := strings.Fields(lower)
	for index, field := range fields {
		key := strings.TrimSuffix(field, ":")
		valueIndex := index + 1
		if valueIndex < len(fields) && fields[valueIndex] == "=" {
			valueIndex++
		}
		if key == "authorization" {
			if valueIndex >= len(fields) || fields[valueIndex] != "[redacted]" {
				return true
			}
		}
		if key == "--token" || key == "--password" || key == "--secret" || key == "--api-key" || key == "token" || key == "password" || key == "secret" || key == "api_key" || key == "api-key" {
			if valueIndex >= len(fields) || fields[valueIndex] != "[redacted]" {
				return true
			}
		}
		if field == "bearer" && (index+1 >= len(fields) || fields[index+1] != "[redacted]") {
			return true
		}
	}
	return false
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

func exceedsSQLiteRange(values ...uint64) bool {
	for _, value := range values {
		if value > math.MaxInt64 {
			return true
		}
	}
	return false
}
