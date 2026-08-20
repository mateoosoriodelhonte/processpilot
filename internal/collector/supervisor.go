package collector

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"github.com/mateoosoriodelhonte/processpilot/internal/protocol"
)

const (
	minimumInterval = 500 * time.Millisecond
	maximumInterval = 60 * time.Second
)

var ErrCollectorOutput = errors.New("invalid collector output")

type commandFunc func(context.Context, string, ...string) *exec.Cmd

type Supervisor struct {
	collectorPath string
	intervalMS    int64
	execCommand   commandFunc
}

func NewSupervisor(collectorPath string, interval time.Duration) (*Supervisor, error) {
	if !filepath.IsAbs(collectorPath) || filepath.Base(filepath.Clean(collectorPath)) != "processpilot-collector" {
		return nil, errors.New("collector path must be absolute and name processpilot-collector")
	}
	if interval < minimumInterval || interval > maximumInterval || interval%time.Millisecond != 0 {
		return nil, fmt.Errorf("collector interval must be a whole millisecond between %s and %s", minimumInterval, maximumInterval)
	}

	return &Supervisor{
		collectorPath: filepath.Clean(collectorPath),
		intervalMS:    interval.Milliseconds(),
		execCommand:   exec.CommandContext,
	}, nil
}

func (s *Supervisor) Sample(ctx context.Context) (protocol.Snapshot, error) {
	var snapshots []protocol.Snapshot
	err := s.run(ctx, true, func(snapshot protocol.Snapshot) error {
		snapshots = append(snapshots, snapshot)
		if len(snapshots) > 1 {
			return fmt.Errorf("%w: --once emitted more than one snapshot", ErrCollectorOutput)
		}
		return nil
	})
	if err != nil {
		return protocol.Snapshot{}, err
	}
	if len(snapshots) != 1 {
		return protocol.Snapshot{}, fmt.Errorf("%w: --once emitted no snapshot", ErrCollectorOutput)
	}
	return snapshots[0], nil
}

func (s *Supervisor) Stream(ctx context.Context, handle func(protocol.Snapshot) error) error {
	if handle == nil {
		return errors.New("collector snapshot handler is required")
	}
	return s.run(ctx, false, handle)
}

func (s *Supervisor) run(ctx context.Context, once bool, handle func(protocol.Snapshot) error) error {
	command := s.command(ctx, once)
	stdout, err := command.StdoutPipe()
	if err != nil {
		return fmt.Errorf("open collector stdout: %w", err)
	}
	command.Stderr = io.Discard
	if err := command.Start(); err != nil {
		return fmt.Errorf("start trusted collector: %w", err)
	}

	ingestErr := ingest(stdout, handle)
	_ = stdout.Close()
	waitErr := command.Wait()
	if ingestErr != nil {
		return ingestErr
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if waitErr != nil {
		return fmt.Errorf("trusted collector exited unsuccessfully: %w", waitErr)
	}
	return nil
}

func (s *Supervisor) command(ctx context.Context, once bool) *exec.Cmd {
	if once {
		return s.execCommand(ctx, s.collectorPath, "--once")
	}
	return s.execCommand(ctx, s.collectorPath, "--interval-ms", strconv.FormatInt(s.intervalMS, 10))
}

func ingest(reader io.Reader, handle func(protocol.Snapshot) error) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), protocol.MaxLineBytes)
	line := 0
	for scanner.Scan() {
		line++
		snapshot, err := protocol.Decode(scanner.Bytes())
		if err != nil {
			return fmt.Errorf("collector line %d: %w", line, err)
		}
		if err := handle(snapshot); err != nil {
			return fmt.Errorf("handle collector line %d: %w", line, err)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("%w: %v", ErrCollectorOutput, err)
	}
	return nil
}
