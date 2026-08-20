package protocol

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDecodeAcceptsSharedRustFixture(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/protocol-v1.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	snapshot, err := Decode(raw)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	if snapshot.ProtocolVersion != Version {
		t.Fatalf("ProtocolVersion = %d, want %d", snapshot.ProtocolVersion, Version)
	}
	if got := snapshot.Processes[0].Name; got != "ollama runner" {
		t.Fatalf("first process name = %q, want ollama runner", got)
	}
}

func TestDecodeRejectsProtocolVersionMismatchClearly(t *testing.T) {
	raw := validSnapshotJSON()
	raw = strings.Replace(raw, `"protocolVersion":1`, `"protocolVersion":2`, 1)

	_, err := Decode([]byte(raw))

	if !errors.Is(err, ErrUnsupportedVersion) {
		t.Fatalf("Decode() error = %v, want ErrUnsupportedVersion", err)
	}
}

func TestDecodeRejectsUnknownFields(t *testing.T) {
	raw := strings.Replace(validSnapshotJSON(), `"sequence":1`, `"sequence":1,"rawCommandLine":"--token secret"`, 1)

	_, err := Decode([]byte(raw))

	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("Decode() error = %v, want unknown field", err)
	}
}

func TestDecodeRejectsDuplicateObjectKeys(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{
			name: "top level",
			raw:  strings.Replace(validSnapshotJSON(), `"sequence":1`, `"sequence":1,"sequence":2`, 1),
		},
		{
			name: "nested process",
			raw:  strings.Replace(validSnapshotJSON(), `"pid":2`, `"pid":2,"pid":3`, 1),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Decode([]byte(tt.raw))
			if err == nil || !strings.Contains(err.Error(), "duplicate object key") {
				t.Fatalf("Decode() error = %v, want duplicate object key rejection", err)
			}
		})
	}
}

func TestDecodeRejectsMalformedAndOversizedCollectorData(t *testing.T) {
	tests := []struct {
		name string
		raw  []byte
	}{
		{name: "malformed", raw: []byte(`{"protocolVersion":`)},
		{name: "invalid UTF-8", raw: []byte("{\"name\":\"\xff\"}")},
		{name: "empty", raw: nil},
		{name: "oversized", raw: []byte(strings.Repeat("x", MaxLineBytes+1))},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Decode(tt.raw); err == nil {
				t.Fatal("Decode() error = nil, want rejection")
			}
		})
	}
}

func TestDecodeRejectsMissingOrUnsafeRequiredData(t *testing.T) {
	futureTimestamp := time.Now().UTC().Add(10 * time.Minute).UnixMilli()
	tests := []struct {
		name string
		raw  string
	}{
		{name: "zero sequence", raw: strings.Replace(validSnapshotJSON(), `"sequence":1`, `"sequence":0`, 1)},
		{name: "zero parent PID", raw: strings.Replace(validSnapshotJSON(), `"parentPid":1`, `"parentPid":0`, 1)},
		{name: "missing truncation flag", raw: strings.Replace(validSnapshotJSON(), `,"processesTruncated":false`, "", 1)},
		{name: "null truncation flag", raw: strings.Replace(validSnapshotJSON(), `"processesTruncated":false`, `"processesTruncated":null`, 1)},
		{name: "missing system CPU", raw: strings.Replace(validSnapshotJSON(), `,"cpuPercent":10`, "", 1)},
		{name: "null system load", raw: strings.Replace(validSnapshotJSON(), `"loadAverage1":1`, `"loadAverage1":null`, 1)},
		{name: "null process CPU", raw: strings.Replace(validSnapshotJSON(), `"cpuPercent":1`, `"cpuPercent":null`, 1)},
		{name: "missing processes", raw: strings.Replace(validSnapshotJSON(), `,"processes":[{"pid":2,"parentPid":1,"name":"known","executable":"/Applications/Known.app/Known","cpuPercent":1,"memoryBytes":10,"startTimeUnixSeconds":1787250000,"status":"Run"}]`, "", 1)},
		{name: "null processes", raw: snapshotWithNullProcesses()},
		{name: "future timestamp", raw: strings.Replace(validSnapshotJSON(), `"timestampUnixMs":1787256000000`, fmt.Sprintf(`"timestampUnixMs":%d`, futureTimestamp), 1)},
		{name: "control in name", raw: strings.Replace(validSnapshotJSON(), `"name":"known"`, `"name":"known\u001b[2J"`, 1)},
		{name: "raw home path", raw: strings.Replace(validSnapshotJSON(), `/Applications/Known.app/Known`, `/Users/alice/private-project/Known`, 1)},
		{name: "case-variant raw home path", raw: strings.Replace(validSnapshotJSON(), `/Applications/Known.app/Known`, `/users/alice/private-project/Known`, 1)},
		{name: "data-volume raw home path", raw: strings.Replace(validSnapshotJSON(), `/Applications/Known.app/Known`, `/System/Volumes/Data/Users/alice/private-project/Known`, 1)},
		{name: "arbitrary absolute path", raw: strings.Replace(validSnapshotJSON(), `/Applications/Known.app/Known`, `/Volumes/PrivateProject/Known`, 1)},
		{name: "relative executable path", raw: strings.Replace(validSnapshotJSON(), `/Applications/Known.app/Known`, `private-project/Known`, 1)},
		{name: "nested private placeholder", raw: strings.Replace(validSnapshotJSON(), `/Applications/Known.app/Known`, `/<private>/project/Known`, 1)},
		{name: "padded traversal", raw: strings.Replace(validSnapshotJSON(), `/Applications/Known.app/Known`, `~/Applications/.. /private-project/Known`, 1)},
		{name: "noncanonical separator", raw: strings.Replace(validSnapshotJSON(), `/Applications/Known.app/Known`, `/Applications//Known.app/Known`, 1)},
		{name: "mixed redacted and raw secret", raw: strings.Replace(validSnapshotJSON(), `"name":"known"`, `"name":"worker token=[REDACTED] https://host/?password=hunter2"`, 1)},
		{name: "redaction prefix with secret suffix", raw: strings.Replace(validSnapshotJSON(), `"name":"known"`, `"name":"worker token=[REDACTED]live-secret"`, 1)},
		{name: "separate raw secret", raw: strings.Replace(validSnapshotJSON(), `"name":"known"`, `"name":"worker --token live-secret"`, 1)},
		{name: "spaced raw secret", raw: strings.Replace(validSnapshotJSON(), `"name":"known"`, `"name":"worker token = live-secret"`, 1)},
		{name: "URL credentials", raw: strings.Replace(validSnapshotJSON(), `"name":"known"`, `"name":"https://alice:hunter2@example.test"`, 1)},
		{name: "compact authorization", raw: strings.Replace(validSnapshotJSON(), `"name":"known"`, `"name":"worker Authorization:Bearer live-secret"`, 1)},
		{name: "empty executable", raw: strings.Replace(validSnapshotJSON(), `/Applications/Known.app/Known`, ` `, 1)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Decode([]byte(tt.raw)); err == nil {
				t.Fatal("Decode() error = nil, want rejection")
			}
		})
	}
}

func TestDecodeRejectsAggregateProcessMemoryOutsideSQLiteRange(t *testing.T) {
	process := `{"pid":2,"parentPid":1,"name":"known","executable":"/Applications/Known.app/Known","cpuPercent":1,"memoryBytes":9223372036854775807,"startTimeUnixSeconds":1787250000,"status":"Run"}`
	second := strings.Replace(process, `"pid":2`, `"pid":3`, 1)
	raw := strings.Replace(validSnapshotJSON(), `"totalMemoryBytes":100`, `"totalMemoryBytes":9223372036854775807`, 1)
	raw = strings.Replace(raw, `"usedMemoryBytes":50`, `"usedMemoryBytes":0`, 1)
	raw = strings.Replace(raw, `"availableMemoryBytes":50`, `"availableMemoryBytes":9223372036854775807`, 1)
	raw = strings.Replace(raw, `[{"pid":2,"parentPid":1,"name":"known","executable":"/Applications/Known.app/Known","cpuPercent":1,"memoryBytes":10,"startTimeUnixSeconds":1787250000,"status":"Run"}]`, `[`+process+`,`+second+`]`, 1)

	if _, err := Decode([]byte(raw)); err == nil {
		t.Fatal("Decode() error = nil, want aggregate range rejection")
	}
}

func TestSnapshotHasNoRawCommandLineField(t *testing.T) {
	processType := reflect.TypeOf(ProcessSample{})
	for i := 0; i < processType.NumField(); i++ {
		field := processType.Field(i)
		if strings.Contains(strings.ToLower(field.Name), "command") {
			t.Fatalf("ProcessSample unexpectedly exposes command field %q", field.Name)
		}
	}
}

func TestDecodeRejectsUnsignedValuesOutsideSQLiteRange(t *testing.T) {
	raw := strings.Replace(validSnapshotJSON(), `"totalMemoryBytes":100`, `"totalMemoryBytes":18446744073709551615`, 1)

	_, err := Decode([]byte(raw))

	if err == nil || !strings.Contains(err.Error(), "SQLite") {
		t.Fatalf("Decode() error = %v, want SQLite range rejection", err)
	}
}

func validSnapshotJSON() string {
	return `{"protocolVersion":1,"timestampUnixMs":1787256000000,"sequence":1,"system":{"totalMemoryBytes":100,"usedMemoryBytes":50,"availableMemoryBytes":50,"totalSwapBytes":0,"usedSwapBytes":0,"cpuPercent":10,"loadAverage1":1,"loadAverage5":1,"loadAverage15":1,"logicalCpuCount":8},"processesTruncated":false,"processes":[{"pid":2,"parentPid":1,"name":"known","executable":"/Applications/Known.app/Known","cpuPercent":1,"memoryBytes":10,"startTimeUnixSeconds":1787250000,"status":"Run"}]}`
}

func snapshotWithNullProcesses() string {
	raw := validSnapshotJSON()
	index := strings.Index(raw, `,"processes":`)
	return raw[:index] + `,"processes":null}`
}
