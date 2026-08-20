package analysis

import (
	"path/filepath"
	"strings"

	"github.com/mateoosoriodelhonte/processpilot/internal/protocol"
)

type Category string

const (
	CategoryBrowser           Category = "BROWSER"
	CategoryBrowserHelper     Category = "BROWSER_HELPER"
	CategoryIDE               Category = "IDE_EDITOR"
	CategoryDevelopmentServer Category = "DEVELOPMENT_SERVER"
	CategoryCompiler          Category = "COMPILER"
	CategoryDatabase          Category = "DATABASE"
	CategoryContainerRuntime  Category = "CONTAINER_RUNTIME"
	CategoryVirtualMachine    Category = "VIRTUAL_MACHINE"
	CategoryAIInference       Category = "AI_INFERENCE"
	CategoryLanguageServer    Category = "LANGUAGE_SERVER"
	CategorySystemService     Category = "SYSTEM_SERVICE"
	CategoryUserApplication   Category = "USER_APPLICATION"
	CategoryUnknown           Category = "UNKNOWN"
)

type Risk string

const (
	RiskLow     Risk = "LOW"
	RiskMedium  Risk = "MEDIUM"
	RiskHigh    Risk = "HIGH"
	RiskUnknown Risk = "UNKNOWN"
)

type Classification struct {
	Application     string   `json:"application"`
	Category        Category `json:"category"`
	Risk            Risk     `json:"stoppingRisk"`
	Reason          string   `json:"reason"`
	PotentialImpact string   `json:"potentialImpact"`
}

func Classify(process protocol.ProcessSample) Classification {
	name := strings.ToLower(strings.TrimSpace(process.Name))
	executableBase := ""
	if process.Executable != nil {
		executableBase = strings.ToLower(filepath.Base(*process.Executable))
	}
	bundle := strings.ToLower(bundleApplication(process))

	if process.PID == 1 || isSystemProcess(name) {
		return classification("macOS system", CategorySystemService, RiskHigh,
			"The process matches a core macOS service signature.",
			"Stopping a system service can destabilize the current session or operating system.")
	}
	if matchesExact(name, executableBase, "ollama", "ollama runner", "llama-server") || bundle == "ollama" {
		return classification("Ollama", CategoryAIInference, RiskMedium,
			"The executable identity matches a local AI model runtime.",
			"Stopping it may interrupt active local model inference; it does not uninstall models.")
	}
	if matchesExact(name, executableBase, "limactl", "virtualization.virtualmachine", "vmware-vmx", "prl_vm_app") || strings.HasPrefix(name, "qemu-system-") || strings.HasPrefix(executableBase, "qemu-system-") || bundle == "vmware fusion" || bundle == "parallels desktop" {
		return classification("Virtual machine", CategoryVirtualMachine, RiskHigh,
			"The process matches a virtual machine worker or controller signature.",
			"Stopping it may interrupt services, containers, or unsaved work inside the virtual machine.")
	}
	if container, ok := containerIdentity(name, executableBase, bundle); ok {
		return classification(container, CategoryContainerRuntime, RiskHigh,
			"The process matches a local container runtime signature.",
			"Stopping it may interrupt running containers and the applications that depend on them.")
	}
	if browser, ok := browserIdentity(name, executableBase, bundle); ok {
		category := CategoryBrowser
		if strings.Contains(name, " helper") || strings.Contains(name, "webcontent") || name == "plugin-container" || strings.Contains(name, " cp ") {
			category = CategoryBrowserHelper
		}
		return classification(browser, category, RiskMedium,
			"The executable identity matches a web browser or one of its helper processes.",
			"Stopping it may close windows or interrupt downloads, forms, and active web applications.")
	}
	if database, ok := databaseIdentity(name, executableBase); ok {
		return classification(database, CategoryDatabase, RiskHigh,
			"The executable name matches a local database server signature.",
			"Stopping it may interrupt applications and could disrupt in-progress writes.")
	}
	if matchesExact(name, executableBase, "code", "code helper", "xcode", "zed") || bundle == "visual studio code" || bundle == "xcode" || bundle == "zed" {
		return classification(applicationName(process, "Code editor"), CategoryIDE, RiskMedium,
			"The application identity matches a development editor or IDE.",
			"Stopping it may close editor windows, terminals, or unsaved work.")
	}
	if name == "rustc" || name == "clang" || name == "swiftc" || name == "go" {
		application := map[string]string{"rustc": "Rust compiler", "clang": "Clang compiler", "swiftc": "Swift compiler", "go": "Go compiler"}[name]
		return classification(application, CategoryCompiler, RiskLow,
			"The process name exactly matches a compiler executable.",
			"Stopping it normally interrupts the current build and may leave incomplete build output.")
	}
	if matchesExact(name, executableBase, "language-server", "language_server", "rust-analyzer", "gopls", "sourcekit-lsp") {
		return classification(applicationName(process, "Language server"), CategoryLanguageServer, RiskLow,
			"The process matches a developer language-service signature.",
			"Stopping it may temporarily disable editor completion, navigation, or diagnostics.")
	}
	if bundle := bundleApplication(process); bundle != "" {
		return classification(bundle, CategoryUserApplication, RiskUnknown,
			"The executable path identifies an application bundle, but its behavior is not in ProcessPilot's known signatures.",
			"Do not terminate this process based solely on ProcessPilot; active application work may be affected.")
	}

	return classification(process.Name, CategoryUnknown, RiskUnknown,
		"ProcessPilot does not have enough safe metadata to identify this process conservatively.",
		"Do not terminate this process based solely on ProcessPilot.")
}

func containerIdentity(name, executableBase, bundle string) (string, bool) {
	switch {
	case bundle == "docker" || matchesExact(name, executableBase, "docker", "dockerd", "com.docker.backend"):
		return "Docker", true
	case bundle == "colima" || matchesExact(name, executableBase, "colima"):
		return "Colima", true
	case matchesExact(name, executableBase, "containerd"):
		return "containerd", true
	default:
		return "", false
	}
}

func classification(application string, category Category, risk Risk, reason, impact string) Classification {
	return Classification{Application: application, Category: category, Risk: risk, Reason: reason, PotentialImpact: impact}
}

func applicationName(process protocol.ProcessSample, fallback string) string {
	if bundle := bundleApplication(process); bundle != "" {
		return bundle
	}
	return fallback
}

func bundleApplication(process protocol.ProcessSample) string {
	if process.Executable == nil {
		return ""
	}
	for _, segment := range strings.Split(filepath.ToSlash(*process.Executable), "/") {
		if strings.HasSuffix(strings.ToLower(segment), ".app") {
			return strings.TrimSuffix(segment, filepath.Ext(segment))
		}
	}
	return ""
}

func isSystemProcess(name string) bool {
	return containsExact(name, "kernel_task", "launchd", "windowserver", "runningboardd", "syslogd", "opendirectoryd")
}

func browserIdentity(name, executableBase, bundle string) (string, bool) {
	switch {
	case bundle == "firefox" || name == "firefox" || strings.HasPrefix(name, "firefox ") || executableBase == "firefox":
		return "Firefox", true
	case bundle == "google chrome" || name == "google chrome" || strings.HasPrefix(name, "google chrome ") || executableBase == "google chrome":
		return "Google Chrome", true
	case bundle == "chromium" || name == "chromium" || strings.HasPrefix(name, "chromium ") || executableBase == "chromium":
		return "Chromium", true
	case bundle == "safari":
		return "Safari", true
	case bundle == "arc":
		return "Arc", true
	default:
		return "", false
	}
}

func databaseIdentity(name, executableBase string) (string, bool) {
	for _, match := range []struct{ signature, application string }{
		{signature: "postgres", application: "PostgreSQL"},
		{signature: "mysqld", application: "MySQL"},
		{signature: "redis-server", application: "Redis"},
		{signature: "mongod", application: "MongoDB"},
	} {
		if name == match.signature || executableBase == match.signature {
			return match.application, true
		}
	}
	return "", false
}

func matchesExact(name, executableBase string, signatures ...string) bool {
	for _, signature := range signatures {
		if name == signature || executableBase == signature {
			return true
		}
	}
	return false
}

func containsExact(value string, options ...string) bool {
	for _, option := range options {
		if value == option {
			return true
		}
	}
	return false
}
