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
	name := strings.ToLower(process.Name)
	executable := ""
	if process.Executable != nil {
		executable = strings.ToLower(*process.Executable)
	}
	identity := name + " " + executable

	if process.PID == 1 || isSystemProcess(name) {
		return classification("macOS system", CategorySystemService, RiskHigh,
			"The process matches a core macOS service signature.",
			"Stopping a system service can destabilize the current session or operating system.")
	}
	if containsAny(identity, "ollama", "llama-server", "llama runner") {
		return classification("Ollama", CategoryAIInference, RiskMedium,
			"The executable identity matches a local AI model runtime.",
			"Stopping it may interrupt active local model inference; it does not uninstall models.")
	}
	if containsAny(identity, "qemu-system", "virtualization.virtualmachine", "limactl", "vmware", "parallels") {
		return classification("Virtual machine", CategoryVirtualMachine, RiskHigh,
			"The process matches a virtual machine worker or controller signature.",
			"Stopping it may interrupt services, containers, or unsaved work inside the virtual machine.")
	}
	if containsAny(identity, "docker", "colima", "containerd") {
		return classification(applicationName(process, "Container runtime"), CategoryContainerRuntime, RiskHigh,
			"The process matches a local container runtime signature.",
			"Stopping it may interrupt running containers and the applications that depend on them.")
	}
	if browser, ok := browserIdentity(identity); ok {
		category := CategoryBrowser
		if containsAny(name, "helper", "webcontent", "plugin-container", "cp ") {
			category = CategoryBrowserHelper
		}
		return classification(browser, category, RiskMedium,
			"The executable identity matches a web browser or one of its helper processes.",
			"Stopping it may close windows or interrupt downloads, forms, and active web applications.")
	}
	if database, ok := databaseIdentity(identity); ok {
		return classification(database, CategoryDatabase, RiskHigh,
			"The executable name matches a local database server signature.",
			"Stopping it may interrupt applications and could disrupt in-progress writes.")
	}
	if containsAny(identity, "visual studio code", "code helper", "/code.app/", "/xcode.app/", "/zed.app/") {
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
	if containsAny(identity, "language-server", "language_server", "rust-analyzer", "gopls", "sourcekit-lsp") {
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

func browserIdentity(identity string) (string, bool) {
	for _, match := range []struct{ signature, application string }{
		{signature: "firefox", application: "Firefox"},
		{signature: "google chrome", application: "Google Chrome"},
		{signature: "chromium", application: "Chromium"},
		{signature: "/safari.app/", application: "Safari"},
		{signature: "/arc.app/", application: "Arc"},
	} {
		if strings.Contains(identity, match.signature) {
			return match.application, true
		}
	}
	return "", false
}

func databaseIdentity(identity string) (string, bool) {
	for _, match := range []struct{ signature, application string }{
		{signature: "postgres", application: "PostgreSQL"},
		{signature: "mysqld", application: "MySQL"},
		{signature: "redis-server", application: "Redis"},
		{signature: "mongod", application: "MongoDB"},
	} {
		if strings.Contains(identity, match.signature) {
			return match.application, true
		}
	}
	return "", false
}

func containsAny(value string, fragments ...string) bool {
	for _, fragment := range fragments {
		if strings.Contains(value, fragment) {
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
