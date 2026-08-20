package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/mateoosoriodelhonte/processpilot/internal/ai"
	"github.com/mateoosoriodelhonte/processpilot/internal/app"
	"github.com/mateoosoriodelhonte/processpilot/internal/cli"
	collectorclient "github.com/mateoosoriodelhonte/processpilot/internal/collector"
	"github.com/mateoosoriodelhonte/processpilot/internal/demo"
	"github.com/mateoosoriodelhonte/processpilot/internal/protocol"
	"github.com/mateoosoriodelhonte/processpilot/internal/store"
	"github.com/mateoosoriodelhonte/processpilot/internal/web"
)

const (
	modeLive = "live"
	modeDemo = "demo"
)

type serverConfig struct {
	mode           string
	port           int
	interval       time.Duration
	retention      time.Duration
	ollamaModel    string
	ollamaEndpoint string
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, output, errorOutput io.Writer) int {
	if len(args) > 0 && (args[0] == "help" || args[0] == "--help" || args[0] == "-h") {
		writeUsage(output)
		return 0
	}
	if len(args) > 0 && isCLICommand(args[0]) {
		client, err := cli.New(web.DefaultPort)
		if err == nil {
			err = client.Run(context.Background(), args, output)
		}
		if err != nil {
			fmt.Fprintf(errorOutput, "ProcessPilot: %v\n", err)
			return 1
		}
		return 0
	}

	mode := modeLive
	if len(args) > 0 && (args[0] == "serve" || args[0] == "demo") {
		if args[0] == "demo" {
			mode = modeDemo
		}
		args = args[1:]
	}
	config, err := parseServerConfig(mode, args)
	if err != nil {
		fmt.Fprintf(errorOutput, "ProcessPilot: %v\n", err)
		writeUsage(errorOutput)
		return 2
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := runServer(ctx, config, output); err != nil {
		fmt.Fprintf(errorOutput, "ProcessPilot: %v\n", err)
		return 1
	}
	return 0
}

func isCLICommand(command string) bool {
	switch command {
	case "status", "top", "inspect", "explain":
		return true
	default:
		return false
	}
}

func parseServerConfig(mode string, args []string) (serverConfig, error) {
	config := serverConfig{
		mode: mode, port: web.DefaultPort, interval: 2 * time.Second, retention: store.DefaultRetention,
		ollamaEndpoint: "http://127.0.0.1:11434",
	}
	flags := flag.NewFlagSet("processpilot", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.IntVar(&config.port, "port", config.port, "loopback dashboard port")
	flags.DurationVar(&config.interval, "interval", config.interval, "collection interval")
	flags.DurationVar(&config.retention, "retention", config.retention, "local telemetry retention")
	flags.StringVar(&config.ollamaModel, "ollama-model", "", "optional local Ollama model")
	flags.StringVar(&config.ollamaEndpoint, "ollama-endpoint", config.ollamaEndpoint, "loopback Ollama endpoint")
	if err := flags.Parse(normalizeDurationArgs(args)); err != nil {
		return serverConfig{}, err
	}
	if flags.NArg() != 0 {
		return serverConfig{}, fmt.Errorf("unexpected argument %q", flags.Arg(0))
	}
	if mode != modeLive && mode != modeDemo {
		return serverConfig{}, errors.New("invalid server mode")
	}
	if _, err := web.Address(config.port); err != nil {
		return serverConfig{}, err
	}
	if config.interval < 500*time.Millisecond || config.interval > 60*time.Second || config.interval%time.Millisecond != 0 {
		return serverConfig{}, errors.New("interval must be a whole millisecond between 500ms and 1m")
	}
	if config.retention < store.MinimumRetention || config.retention > store.MaximumRetention {
		return serverConfig{}, fmt.Errorf("retention must be between %s and %s", store.MinimumRetention, store.MaximumRetention)
	}
	if mode == modeDemo && config.ollamaModel != "" {
		return serverConfig{}, errors.New("demo mode uses deterministic NoAI explanations")
	}
	defaultEndpoint := "http://127.0.0.1:11434"
	if config.ollamaModel == "" && config.ollamaEndpoint != defaultEndpoint {
		return serverConfig{}, errors.New("ollama-endpoint requires ollama-model")
	}
	if config.ollamaModel != "" {
		if _, err := ai.NewOllamaProvider(config.ollamaEndpoint, config.ollamaModel, nil); err != nil {
			return serverConfig{}, err
		}
	}
	return config, nil
}

// The flag package accepts Go duration syntax. This adds the documentation-friendly
// day suffix without widening any runtime boundary.
func normalizeDurationArgs(args []string) []string {
	result := append([]string(nil), args...)
	for index := range result {
		if result[index] == "--retention" && index+1 < len(result) {
			result[index+1] = expandDays(result[index+1])
		} else if len(result[index]) > len("--retention=") && result[index][:len("--retention=")] == "--retention=" {
			result[index] = "--retention=" + expandDays(result[index][len("--retention="):])
		}
	}
	return result
}

func expandDays(value string) string {
	raw, found := strings.CutSuffix(value, "d")
	if !found {
		return value
	}
	days, err := strconv.ParseInt(raw, 10, 16)
	if err == nil && days >= 0 && days <= int64(store.MaximumRetention/(24*time.Hour)) {
		return fmt.Sprintf("%dh", days*24)
	}
	return value
}

func runServer(ctx context.Context, config serverConfig, output io.Writer) error {
	address, err := web.Address(config.port)
	if err != nil {
		return err
	}

	var service *app.Service
	var producer func(context.Context) error
	var closeStore func() error
	if config.mode == modeDemo {
		anchor := time.Now().UTC()
		demoStore := demo.NewStore(anchor)
		service = app.NewService(demoStore, ai.NoAIProvider{}, true)
		if err := service.Accept(ctx, demo.Snapshot(anchor, 1)); err != nil {
			return fmt.Errorf("initialize demo data: %w", err)
		}
		producer = demoProducer(service, config.interval, 1)
	} else {
		service, producer, closeStore, err = liveRuntime(ctx, config)
		if err != nil {
			return err
		}
		defer func() { _ = closeStore() }()
	}

	listener, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("listen on private dashboard address %s: %w", address, err)
	}
	defer listener.Close()

	if config.mode == modeDemo {
		fmt.Fprintln(output, "ProcessPilot is running with Demo data. These values are not telemetry from this Mac.")
	} else {
		fmt.Fprintln(output, "ProcessPilot is observing this Mac without administrator privileges.")
	}
	fmt.Fprintf(output, "Dashboard: http://%s\nPress Control-C to stop ProcessPilot.\n", address)

	runtimeCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	producerErrors := make(chan error, 1)
	go func() { producerErrors <- producer(runtimeCtx) }()

	httpServer := &http.Server{
		Handler: web.NewWithSettings(service, web.Settings{
			Interval: config.interval, Retention: config.retention, ExplanationProvider: explanationProviderName(config),
		}).Handler(), ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10,
	}
	serverErrors := make(chan error, 1)
	go func() { serverErrors <- httpServer.Serve(listener) }()

	var runtimeErr error
	select {
	case <-ctx.Done():
	case err := <-producerErrors:
		if err != nil && !errors.Is(err, context.Canceled) {
			runtimeErr = fmt.Errorf("telemetry producer stopped: %w", err)
		}
	case err := <-serverErrors:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			runtimeErr = fmt.Errorf("dashboard server stopped: %w", err)
		}
	}
	cancel()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil && runtimeErr == nil {
		runtimeErr = fmt.Errorf("shut down dashboard: %w", err)
	}
	return runtimeErr
}

func explanationProviderName(config serverConfig) string {
	if config.ollamaModel != "" {
		return "Ollama with NoAI fallback"
	}
	return "No AI"
}

func liveRuntime(ctx context.Context, config serverConfig) (*app.Service, func(context.Context) error, func() error, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("locate ProcessPilot executable: %w", err)
	}
	collectorPath, err := resolveCollectorPath(executable)
	if err != nil {
		return nil, nil, nil, err
	}
	supervisor, err := collectorclient.NewSupervisor(collectorPath, config.interval)
	if err != nil {
		return nil, nil, nil, err
	}

	configDirectory, err := os.UserConfigDir()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("locate local application data: %w", err)
	}
	dataDirectory := filepath.Join(configDirectory, "ProcessPilot")
	if err := os.MkdirAll(dataDirectory, 0o700); err != nil {
		return nil, nil, nil, fmt.Errorf("create ProcessPilot data directory: %w", err)
	}
	if err := os.Chmod(dataDirectory, 0o700); err != nil {
		return nil, nil, nil, fmt.Errorf("protect ProcessPilot data directory: %w", err)
	}
	databasePath := filepath.Join(dataDirectory, "processpilot.db")
	liveStore, err := store.Open(databasePath, config.retention)
	if err != nil {
		return nil, nil, nil, err
	}
	if err := os.Chmod(databasePath, 0o600); err != nil {
		_ = liveStore.Close()
		return nil, nil, nil, fmt.Errorf("protect ProcessPilot database: %w", err)
	}

	var provider ai.Provider = ai.NoAIProvider{}
	if config.ollamaModel != "" {
		ollama, providerErr := ai.NewOllamaProvider(config.ollamaEndpoint, config.ollamaModel, nil)
		if providerErr != nil {
			_ = liveStore.Close()
			return nil, nil, nil, providerErr
		}
		provider = ai.FallbackProvider{Primary: ollama, Fallback: ai.NoAIProvider{}}
	}
	service := app.NewService(liveStore, provider, false)
	first, err := supervisor.Sample(ctx)
	if err != nil {
		_ = liveStore.Close()
		return nil, nil, nil, fmt.Errorf("collect initial telemetry: %w", err)
	}
	if err := service.Accept(ctx, first); err != nil {
		_ = liveStore.Close()
		return nil, nil, nil, err
	}
	producer := func(streamCtx context.Context) error {
		return supervisor.Stream(streamCtx, func(snapshot protocol.Snapshot) error {
			return service.Accept(streamCtx, snapshot)
		})
	}
	return service, producer, liveStore.Close, nil
}

func resolveCollectorPath(executable string) (string, error) {
	resolved, err := filepath.EvalSymlinks(executable)
	if err != nil {
		return "", fmt.Errorf("resolve ProcessPilot executable: %w", err)
	}
	collectorPath := filepath.Join(filepath.Dir(resolved), "processpilot-collector")
	info, err := os.Lstat(collectorPath)
	if err != nil {
		return "", fmt.Errorf("find packaged processpilot-collector beside ProcessPilot: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return "", errors.New("packaged processpilot-collector must be a regular executable file")
	}
	return collectorPath, nil
}

func demoProducer(service *app.Service, interval time.Duration, sequence uint64) func(context.Context) error {
	return func(ctx context.Context) error {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case at := <-ticker.C:
				sequence++
				if err := service.Accept(ctx, demo.Snapshot(at.UTC(), sequence)); err != nil {
					return err
				}
			}
		}
	}
}

func writeUsage(output io.Writer) {
	fmt.Fprintln(output, `Usage:
  processpilot [serve] [--port 7345] [--interval 2s] [--retention 7d]
  processpilot demo [--port 7345] [--interval 2s]
  processpilot status
  processpilot top
  processpilot inspect PID
  processpilot explain PID

ProcessPilot is read-only. It has no kill, stop, restart, or command execution interface.`)
}
