package analysis

import (
	"slices"
	"testing"

	"github.com/mateoosoriodelhonte/processpilot/internal/protocol"
)

func TestBuildGroupsRelatedProcessesAndReconcilesTotals(t *testing.T) {
	parent := sampleProcess(100, "Firefox", "/Applications/Firefox.app/Contents/MacOS/firefox")
	parent.MemoryBytes = 100
	parent.CPUPercent = 2
	helper := sampleProcess(101, "firefox cp webcontent", "/Applications/Firefox.app/Contents/MacOS/plugin-container")
	helper.ParentPID = uint32Pointer(100)
	helper.MemoryBytes = 250
	helper.CPUPercent = 3

	result := Build([]protocol.ProcessSample{helper, parent})

	if len(result.Applications) != 1 {
		t.Fatalf("applications = %#v", result.Applications)
	}
	application := result.Applications[0]
	if application.Name != "Firefox" || application.ProcessCount != 2 || application.MemoryBytes != 350 || application.CPUPercent != 5 {
		t.Fatalf("application = %#v", application)
	}
	if !slices.Equal(application.PIDs, []uint32{100, 101}) {
		t.Fatalf("PIDs = %v", application.PIDs)
	}
}

func TestBuildDoesNotOverGroupUnknownProcesses(t *testing.T) {
	first := sampleProcess(201, "worker", "~/<private>/worker")
	second := sampleProcess(202, "worker", "~/<private>/worker")

	result := Build([]protocol.ProcessSample{first, second})

	if len(result.Applications) != 2 {
		t.Fatalf("unknown applications were over-grouped: %#v", result.Applications)
	}
	if result.Applications[0].Key == result.Applications[1].Key {
		t.Fatalf("unknown applications share a storage key: %#v", result.Applications)
	}
	for _, application := range result.Applications {
		if application.Risk != RiskUnknown || application.Category != CategoryUnknown {
			t.Fatalf("unknown application lost conservative classification: %#v", application)
		}
	}
}

func TestOwnershipTraversalHandlesCyclesAndMissingParents(t *testing.T) {
	first := sampleProcess(301, "cycle-a", "~/<private>/cycle-a")
	second := sampleProcess(302, "cycle-b", "~/<private>/cycle-b")
	first.ParentPID = uint32Pointer(302)
	second.ParentPID = uint32Pointer(301)
	missing := sampleProcess(303, "orphan", "~/<private>/orphan")
	missing.ParentPID = uint32Pointer(999)
	knownMissing := sampleProcess(304, "qemu-system-aarch64", "/opt/homebrew/bin/qemu-system-aarch64")
	knownMissing.ParentPID = uint32Pointer(999)

	result := Build([]protocol.ProcessSample{first, second, missing, knownMissing})

	for _, process := range result.Processes {
		if len(process.Ownership.Chain) > 3 {
			t.Fatalf("ownership chain was not bounded: %#v", process.Ownership)
		}
		if process.Ownership.Confidence != ConfidenceUnknown {
			t.Fatalf("malformed graph gained confidence: %#v", process.Ownership)
		}
	}
}

func TestOwnershipPrefersTheOutermostRecognizedApplicationButNotLaunchd(t *testing.T) {
	qemu := sampleProcess(400, "qemu-system-aarch64", "/opt/homebrew/bin/qemu-system-aarch64")
	limactl := sampleProcess(401, "limactl", "/opt/homebrew/bin/limactl")
	colima := sampleProcess(402, "colima", "/opt/homebrew/bin/colima")
	qemu.ParentPID = uint32Pointer(401)
	limactl.ParentPID = uint32Pointer(402)

	launchd := sampleProcess(1, "launchd", "/sbin/launchd")
	unknown := sampleProcess(500, "private-worker", "~/<private>/private-worker")
	unknown.ParentPID = uint32Pointer(1)

	result := Build([]protocol.ProcessSample{qemu, limactl, colima, unknown, launchd})
	byPID := make(map[uint32]Process)
	for _, process := range result.Processes {
		byPID[process.Observed.PID] = process
	}
	if got := byPID[400].Ownership.Application; got != "Colima" {
		t.Fatalf("qemu owner = %q, want Colima", got)
	}
	if got := byPID[500].Ownership.Confidence; got != ConfidenceUnknown {
		t.Fatalf("unknown launchd child confidence = %q, want UNKNOWN", got)
	}
}

func uint32Pointer(value uint32) *uint32 {
	return &value
}
