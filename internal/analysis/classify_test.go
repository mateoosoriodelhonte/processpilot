package analysis

import (
	"strings"
	"testing"

	"github.com/mateoosoriodelhonte/processpilot/internal/protocol"
)

func TestClassifyUsesDeterministicKnownSignatures(t *testing.T) {
	tests := []struct {
		name        string
		process     protocol.ProcessSample
		application string
		category    Category
		risk        Risk
	}{
		{name: "ollama runner", process: sampleProcess(10, "ollama runner", "/Applications/Ollama.app/Contents/MacOS/ollama"), application: "Ollama", category: CategoryAIInference, risk: RiskMedium},
		{name: "firefox helper", process: sampleProcess(11, "firefox cp webcontent", "/Applications/Firefox.app/Contents/MacOS/plugin-container"), application: "Firefox", category: CategoryBrowserHelper, risk: RiskMedium},
		{name: "postgres", process: sampleProcess(12, "postgres", "/opt/homebrew/bin/postgres"), application: "PostgreSQL", category: CategoryDatabase, risk: RiskHigh},
		{name: "colima", process: sampleProcess(13, "qemu-system-aarch64", "/opt/homebrew/bin/qemu-system-aarch64"), application: "Virtual machine", category: CategoryVirtualMachine, risk: RiskHigh},
		{name: "rust compiler", process: sampleProcess(14, "rustc", "/Users/alice/.rustup/toolchains/stable/bin/rustc"), application: "Rust compiler", category: CategoryCompiler, risk: RiskLow},
		{name: "launchd", process: sampleProcess(1, "launchd", "/sbin/launchd"), application: "macOS system", category: CategorySystemService, risk: RiskHigh},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			classification := Classify(tt.process)
			if classification.Application != tt.application || classification.Category != tt.category || classification.Risk != tt.risk {
				t.Fatalf("Classify() = %#v", classification)
			}
			if classification.Reason == "" || classification.PotentialImpact == "" {
				t.Fatalf("classification lacks evidence: %#v", classification)
			}
		})
	}
}

func TestUnknownProcessRemainsConservativeAndUnknown(t *testing.T) {
	classification := Classify(sampleProcess(99, "mystery-worker", "~/<private>/mystery-worker"))

	if classification.Category != CategoryUnknown || classification.Risk != RiskUnknown {
		t.Fatalf("Classify(unknown) = %#v", classification)
	}
	combined := strings.ToLower(classification.Reason + " " + classification.PotentialImpact)
	if !strings.Contains(combined, "do not terminate") {
		t.Fatalf("unknown guidance is not conservative: %q", combined)
	}
	if strings.Contains(combined, "safe to kill") {
		t.Fatalf("forbidden certainty in guidance: %q", combined)
	}
}

func sampleProcess(pid uint32, name, executable string) protocol.ProcessSample {
	return protocol.ProcessSample{
		PID:                  pid,
		Name:                 name,
		Executable:           &executable,
		CPUPercent:           1,
		MemoryBytes:          10,
		StartTimeUnixSeconds: 1_787_250_000,
		Status:               "Run",
	}
}
