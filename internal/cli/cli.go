package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/mateoosoriodelhonte/processpilot/internal/ai"
	"github.com/mateoosoriodelhonte/processpilot/internal/analysis"
	"github.com/mateoosoriodelhonte/processpilot/internal/protocol"
	"github.com/mateoosoriodelhonte/processpilot/internal/web"
)

const maximumResponseBytes = 2 << 20

type Client struct {
	baseURL    string
	httpClient *http.Client
}

func New(port int) (*Client, error) {
	address, err := web.Address(port)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 5 * time.Second}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{baseURL: "http://" + address, httpClient: client}, nil
}

func (client *Client) Run(ctx context.Context, args []string, output io.Writer) error {
	if len(args) == 0 {
		return usageError()
	}
	switch args[0] {
	case "status":
		if len(args) != 1 {
			return usageError()
		}
		return client.status(ctx, output)
	case "top":
		if len(args) != 1 {
			return usageError()
		}
		return client.top(ctx, output)
	case "inspect", "explain":
		if len(args) != 2 {
			return usageError()
		}
		pid, err := strconv.ParseUint(args[1], 10, 32)
		if err != nil || pid == 0 {
			return errors.New("PID must be a positive integer")
		}
		if args[0] == "inspect" {
			return client.inspect(ctx, uint32(pid), output)
		}
		return client.explain(ctx, uint32(pid), output)
	default:
		return usageError()
	}
}

func (client *Client) status(ctx context.Context, output io.Writer) error {
	var response struct {
		Data struct {
			Observed protocol.SystemSample `json:"observed"`
			Pressure analysis.Pressure     `json:"pressure"`
			Demo     bool                  `json:"demo"`
		} `json:"data"`
	}
	if err := client.get(ctx, "/api/v1/system", &response); err != nil {
		return err
	}
	fmt.Fprintf(output, "ProcessPilot: live (local only)\nPressure: %s — %s\nCPU: %.1f%%\nMemory: %s / %s\nDemo data: %s\n",
		terminalSafe(string(response.Data.Pressure.Level)), terminalSafe(response.Data.Pressure.Summary), response.Data.Observed.CPUPercent,
		formatBytes(response.Data.Observed.UsedMemoryBytes), formatBytes(response.Data.Observed.TotalMemoryBytes), yesNo(response.Data.Demo))
	return nil
}

func (client *Client) top(ctx context.Context, output io.Writer) error {
	var response struct {
		Data []analysis.Application `json:"data"`
		Demo bool                   `json:"demo"`
	}
	if err := client.get(ctx, "/api/v1/applications?page=1&pageSize=20", &response); err != nil {
		return err
	}
	if response.Demo {
		fmt.Fprintln(output, "Demo data")
	}
	fmt.Fprintln(output, "APPLICATION\tCPU\tMEMORY\tPROCESSES\tRISK")
	for _, application := range response.Data {
		fmt.Fprintf(output, "%s\t%.1f%%\t%s\t%d\t%s\n", terminalSafe(application.Name), application.CPUPercent,
			formatBytes(application.MemoryBytes), application.ProcessCount, terminalSafe(string(application.Risk)))
	}
	return nil
}

func (client *Client) inspect(ctx context.Context, pid uint32, output io.Writer) error {
	response, err := client.process(ctx, pid)
	if err != nil {
		return err
	}
	fmt.Fprintf(output, "Observed\n  PID: %d\n  Name: %s\n  CPU: %.1f%%\n  Memory: %s\n  State: %s\n\n",
		response.Observed.PID, terminalSafe(response.Observed.Name), response.Observed.CPUPercent,
		formatBytes(response.Observed.MemoryBytes), terminalSafe(response.Observed.Status))
	fmt.Fprintf(output, "ProcessPilot classification\n  Process identity: %s\n  Grouped application: %s\n  Category: %s\n  Stopping risk: %s\n  Why: %s\n  Potential impact: %s\n",
		terminalSafe(response.Classification.Application), terminalSafe(response.Ownership.Application), terminalSafe(string(response.Classification.Category)), terminalSafe(string(response.Classification.Risk)),
		terminalSafe(response.Classification.Reason), terminalSafe(response.Classification.PotentialImpact))
	return nil
}

func (client *Client) explain(ctx context.Context, pid uint32, output io.Writer) error {
	response, err := client.process(ctx, pid)
	if err != nil {
		return err
	}
	fmt.Fprintf(output, "%s\nProvider: %s\n", terminalSafe(response.Explanation.Text), terminalSafe(response.Explanation.Provider))
	return nil
}

type processDetail struct {
	Observed       protocol.ProcessSample  `json:"observed"`
	Classification analysis.Classification `json:"classification"`
	Ownership      analysis.Ownership      `json:"ownership"`
	Explanation    ai.Explanation          `json:"explanation"`
}

func (client *Client) process(ctx context.Context, pid uint32) (processDetail, error) {
	var response struct {
		Data processDetail `json:"data"`
	}
	if err := client.get(ctx, "/api/v1/processes/"+strconv.FormatUint(uint64(pid), 10), &response); err != nil {
		return processDetail{}, err
	}
	return response.Data, nil
}

func (client *Client) get(ctx context.Context, path string, destination any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, client.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("create local API request: %w", err)
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("ProcessPilot server is unavailable at %s: %w", client.baseURL, err)
	}
	defer response.Body.Close()
	raw, readErr := io.ReadAll(io.LimitReader(response.Body, maximumResponseBytes+1))
	if readErr != nil {
		return fmt.Errorf("read local API response: %w", readErr)
	}
	if len(raw) > maximumResponseBytes {
		return errors.New("local API response exceeds size limit")
	}
	if response.StatusCode != http.StatusOK {
		body := raw
		if len(body) > 4<<10 {
			body = body[:4<<10]
		}
		return fmt.Errorf("local API returned HTTP %d: %s", response.StatusCode, terminalSafe(strings.TrimSpace(string(body))))
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("decode local API response: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("local API returned trailing or oversized data")
	}
	return nil
}

func usageError() error {
	return errors.New("usage: processpilot {status|top|inspect PID|explain PID}")
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func formatBytes(value uint64) string {
	return fmt.Sprintf("%.1f GiB", float64(value)/float64(uint64(1)<<30))
}

func terminalSafe(value string) string {
	value = strings.ToValidUTF8(value, "�")
	return strings.Map(func(character rune) rune {
		if unicode.IsControl(character) || unicode.Is(unicode.Cf, character) {
			return -1
		}
		return character
	}, value)
}
