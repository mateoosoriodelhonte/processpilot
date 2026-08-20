package app

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/mateoosoriodelhonte/processpilot/internal/ai"
	"github.com/mateoosoriodelhonte/processpilot/internal/analysis"
	"github.com/mateoosoriodelhonte/processpilot/internal/protocol"
)

const maximumSubscribers = 32

var (
	ErrNoSnapshot      = errors.New("no telemetry snapshot is available")
	ErrProcessNotFound = errors.New("process was not found in the current snapshot")
)

type HistoryStore interface {
	Record(context.Context, protocol.Snapshot, analysis.Result) error
	Cleanup(context.Context, time.Time) (int64, error)
	AllApplicationHistory(context.Context, time.Time, int) ([]analysis.HistoryPoint, error)
	ApplicationHistory(context.Context, string, time.Time, int) ([]analysis.HistoryPoint, error)
}

type State struct {
	Snapshot  protocol.Snapshot  `json:"observed"`
	Analysis  analysis.Result    `json:"analysis"`
	Pressure  analysis.Pressure  `json:"pressure"`
	Anomalies []analysis.Anomaly `json:"anomalies"`
	Demo      bool               `json:"demo"`
}

type Service struct {
	mutex       sync.RWMutex
	store       HistoryStore
	provider    ai.Provider
	demo        bool
	state       State
	hasState    bool
	subscribers map[uint64]chan State
	nextID      uint64
}

func NewService(store HistoryStore, provider ai.Provider, demo bool) *Service {
	if provider == nil {
		provider = ai.NoAIProvider{}
	}
	return &Service{
		store: store, provider: provider, demo: demo, subscribers: make(map[uint64]chan State),
	}
}

func (service *Service) Accept(ctx context.Context, snapshot protocol.Snapshot) error {
	result := analysis.Build(snapshot.Processes)
	pressure := analysis.AssessPressure(snapshot.System)
	timestamp := time.UnixMilli(int64(snapshot.TimestampUnixMS)).UTC()
	var history []analysis.HistoryPoint

	if service.store != nil {
		var err error
		history, err = service.store.AllApplicationHistory(ctx, timestamp.Add(-30*24*time.Hour), 100_000)
		if err != nil {
			return fmt.Errorf("read anomaly history: %w", err)
		}
		if err := service.store.Record(ctx, snapshot, result); err != nil {
			return fmt.Errorf("persist telemetry: %w", err)
		}
		if _, err := service.store.Cleanup(ctx, timestamp); err != nil {
			return fmt.Errorf("enforce telemetry retention: %w", err)
		}
	}

	state := State{
		Snapshot: snapshot, Analysis: result, Pressure: pressure,
		Anomalies: analysis.DetectAnomalies(history, result.Applications, snapshot.System.TotalMemoryBytes, timestamp),
		Demo:      service.demo,
	}
	service.publish(state)
	return nil
}

func (service *Service) Current() (State, bool) {
	service.mutex.RLock()
	defer service.mutex.RUnlock()
	if !service.hasState {
		return State{}, false
	}
	return cloneState(service.state), true
}

func (service *Service) Process(pid uint32) (analysis.Process, bool) {
	state, ok := service.Current()
	if !ok {
		return analysis.Process{}, false
	}
	for _, process := range state.Analysis.Processes {
		if process.Observed.PID == pid {
			return process, true
		}
	}
	return analysis.Process{}, false
}

func (service *Service) Explain(ctx context.Context, pid uint32) (ai.Explanation, error) {
	state, ok := service.Current()
	if !ok {
		return ai.Explanation{}, ErrNoSnapshot
	}
	var selected *analysis.Process
	for index := range state.Analysis.Processes {
		if state.Analysis.Processes[index].Observed.PID == pid {
			selected = &state.Analysis.Processes[index]
			break
		}
	}
	if selected == nil {
		return ai.Explanation{}, ErrProcessNotFound
	}
	return service.provider.Explain(ctx, ai.Input{
		Application: selected.Ownership.Application, Category: selected.Classification.Category,
		Risk: selected.Classification.Risk, CPUPercent: selected.Observed.CPUPercent,
		MemoryBytes: selected.Observed.MemoryBytes, TotalMemoryBytes: state.Snapshot.System.TotalMemoryBytes,
		DeterministicReason: selected.Classification.Reason, PotentialImpact: selected.Classification.PotentialImpact,
	})
}

func (service *Service) History(ctx context.Context, application string, since time.Time, limit int) ([]analysis.HistoryPoint, error) {
	if service.store == nil {
		return nil, nil
	}
	if application == "" {
		return service.store.AllApplicationHistory(ctx, since, limit)
	}
	return service.store.ApplicationHistory(ctx, application, since, limit)
}

func (service *Service) Subscribe() (<-chan State, func(), error) {
	service.mutex.Lock()
	defer service.mutex.Unlock()
	if len(service.subscribers) >= maximumSubscribers {
		return nil, nil, errors.New("too many live dashboard subscribers")
	}
	service.nextID++
	id := service.nextID
	updates := make(chan State, 1)
	service.subscribers[id] = updates
	if service.hasState {
		updates <- cloneState(service.state)
	}

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			service.mutex.Lock()
			defer service.mutex.Unlock()
			delete(service.subscribers, id)
			close(updates)
		})
	}
	return updates, cancel, nil
}

func (service *Service) publish(state State) {
	service.mutex.Lock()
	defer service.mutex.Unlock()
	service.state = cloneState(state)
	service.hasState = true
	for _, updates := range service.subscribers {
		copy := cloneState(state)
		select {
		case updates <- copy:
		default:
			<-updates
			updates <- copy
		}
	}
}

func cloneState(state State) State {
	copy := state
	copy.Snapshot.Processes = append([]protocol.ProcessSample(nil), state.Snapshot.Processes...)
	for index := range copy.Snapshot.Processes {
		copy.Snapshot.Processes[index] = cloneProcessSample(state.Snapshot.Processes[index])
	}
	copy.Analysis.Processes = append([]analysis.Process(nil), state.Analysis.Processes...)
	for index := range copy.Analysis.Processes {
		copy.Analysis.Processes[index].Observed = cloneProcessSample(state.Analysis.Processes[index].Observed)
		copy.Analysis.Processes[index].Ownership.Chain = append([]uint32(nil), state.Analysis.Processes[index].Ownership.Chain...)
	}
	copy.Analysis.Applications = append([]analysis.Application(nil), state.Analysis.Applications...)
	for index := range copy.Analysis.Applications {
		copy.Analysis.Applications[index].PIDs = append([]uint32(nil), state.Analysis.Applications[index].PIDs...)
	}
	copy.Pressure.Evidence = append([]string(nil), state.Pressure.Evidence...)
	copy.Anomalies = append([]analysis.Anomaly(nil), state.Anomalies...)
	return copy
}

func cloneProcessSample(sample protocol.ProcessSample) protocol.ProcessSample {
	copy := sample
	if sample.ParentPID != nil {
		value := *sample.ParentPID
		copy.ParentPID = &value
	}
	if sample.Executable != nil {
		value := *sample.Executable
		copy.Executable = &value
	}
	return copy
}
