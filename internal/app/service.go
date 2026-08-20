package app

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/mateoosoriodelhonte/processpilot/internal/ai"
	"github.com/mateoosoriodelhonte/processpilot/internal/analysis"
	"github.com/mateoosoriodelhonte/processpilot/internal/protocol"
)

const (
	maximumSubscribers        = 32
	maximumUpdateApplications = 100
)

const persistenceWarning = "Local history is temporarily unavailable. Current read-only telemetry remains live."

var (
	ErrNoSnapshot      = errors.New("no telemetry snapshot is available")
	ErrProcessNotFound = errors.New("process was not found in the current snapshot")
)

type HistoryStore interface {
	Record(context.Context, protocol.Snapshot, analysis.Result) error
	Cleanup(context.Context, time.Time) (int64, error)
	AllApplicationHistory(context.Context, time.Time, int) ([]analysis.HistoryPoint, error)
	AnomalyHistory(context.Context, time.Time, int, []string) ([]analysis.HistoryPoint, error)
	ApplicationHistory(context.Context, string, time.Time, int) ([]analysis.HistoryPoint, error)
	ApplicationKeyHistory(context.Context, string, time.Time, int) ([]analysis.HistoryPoint, error)
}

type State struct {
	Snapshot  protocol.Snapshot  `json:"observed"`
	Analysis  analysis.Result    `json:"analysis"`
	Pressure  analysis.Pressure  `json:"pressure"`
	Anomalies []analysis.Anomaly `json:"anomalies"`
	Demo      bool               `json:"demo"`
	Warning   string             `json:"warning,omitempty"`
}

type Update struct {
	TimestampUnixMS    uint64
	Sequence           uint64
	ProcessesTruncated bool
	Pressure           analysis.Pressure
	Applications       []analysis.Application
	Anomalies          []analysis.Anomaly
	Demo               bool
}

type Service struct {
	ingestMutex     sync.Mutex
	mutex           sync.RWMutex
	store           HistoryStore
	provider        ai.Provider
	demo            bool
	state           State
	hasState        bool
	subscribers     map[uint64]chan Update
	nextID          uint64
	history         []analysis.HistoryPoint
	historyAt       time.Time
	historyDegraded bool
	historyUsable   bool
}

func NewService(store HistoryStore, provider ai.Provider, demo bool) *Service {
	if provider == nil {
		provider = ai.NoAIProvider{}
	}
	return &Service{
		store: store, provider: provider, demo: demo, subscribers: make(map[uint64]chan Update),
	}
}

func (service *Service) Accept(ctx context.Context, snapshot protocol.Snapshot) error {
	service.ingestMutex.Lock()
	defer service.ingestMutex.Unlock()
	result := analysis.Build(snapshot.Processes)
	pressure := analysis.AssessPressure(snapshot.System)
	timestamp := time.UnixMilli(int64(snapshot.TimestampUnixMS)).UTC()
	var history []analysis.HistoryPoint
	degraded := service.historyDegraded

	if service.store != nil {
		if !snapshot.ProcessesTruncated && (service.historyAt.IsZero() || timestamp.Before(service.historyAt) || timestamp.Sub(service.historyAt) >= time.Minute) {
			loaded, err := service.store.AnomalyHistory(ctx, timestamp.Add(-30*24*time.Hour), 10, anomalyApplicationKeys(result.Applications))
			if err != nil {
				degraded = true
				service.historyUsable = false
			} else {
				service.history = loaded
				degraded = false
				service.historyUsable = true
			}
			service.historyAt = timestamp
		}
		history = service.history
		if err := service.store.Record(ctx, snapshot, result); err != nil {
			degraded = true
		}
		if _, err := service.store.Cleanup(ctx, timestamp); err != nil {
			degraded = true
		}
		service.historyDegraded = degraded
	}
	warning := ""
	if degraded {
		warning = persistenceWarning
	}
	anomalies := []analysis.Anomaly(nil)
	if !snapshot.ProcessesTruncated && (service.store == nil || service.historyUsable) {
		applications := result.Applications
		if len(applications) > maximumUpdateApplications {
			applications = applications[:maximumUpdateApplications]
		}
		anomalies = analysis.DetectAnomalies(history, applications, snapshot.System.TotalMemoryBytes, timestamp)
	}

	state := State{
		Snapshot: snapshot, Analysis: result, Pressure: pressure,
		Anomalies: anomalies,
		Demo:      service.demo, Warning: warning,
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

func (service *Service) Application(key string) (analysis.Application, bool) {
	state, ok := service.Current()
	if !ok {
		return analysis.Application{}, false
	}
	for _, application := range state.Analysis.Applications {
		if application.Key == key {
			return application, true
		}
	}
	return analysis.Application{}, false
}

func (service *Service) Explain(ctx context.Context, pid uint32) (ai.Explanation, error) {
	_, explanation, err := service.ProcessDetail(ctx, pid)
	return explanation, err
}

func (service *Service) ProcessDetail(ctx context.Context, pid uint32) (analysis.Process, ai.Explanation, error) {
	state, ok := service.Current()
	if !ok {
		return analysis.Process{}, ai.Explanation{}, ErrNoSnapshot
	}
	var selected *analysis.Process
	for index := range state.Analysis.Processes {
		if state.Analysis.Processes[index].Observed.PID == pid {
			selected = &state.Analysis.Processes[index]
			break
		}
	}
	if selected == nil {
		return analysis.Process{}, ai.Explanation{}, ErrProcessNotFound
	}
	explanation, err := service.provider.Explain(ctx, ai.Input{
		Application: selected.Classification.Application, Category: selected.Classification.Category,
		Risk: selected.Classification.Risk, CPUPercent: selected.Observed.CPUPercent,
		MemoryBytes: selected.Observed.MemoryBytes, TotalMemoryBytes: state.Snapshot.System.TotalMemoryBytes,
		DeterministicReason: selected.Classification.Reason, PotentialImpact: selected.Classification.PotentialImpact,
	})
	if err != nil {
		return analysis.Process{}, ai.Explanation{}, err
	}
	return *selected, explanation, nil
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

func (service *Service) ApplicationHistory(ctx context.Context, applicationKey string, since time.Time, limit int) ([]analysis.HistoryPoint, error) {
	if service.store == nil {
		return nil, nil
	}
	return service.store.ApplicationKeyHistory(ctx, applicationKey, since, limit)
}

func (service *Service) Subscribe() (<-chan Update, func(), error) {
	service.mutex.Lock()
	defer service.mutex.Unlock()
	if len(service.subscribers) >= maximumSubscribers {
		return nil, nil, errors.New("too many live dashboard subscribers")
	}
	service.nextID++
	id := service.nextID
	updates := make(chan Update, 1)
	service.subscribers[id] = updates
	if service.hasState {
		updates <- cloneUpdate(stateUpdate(service.state))
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
	update := stateUpdate(state)
	for _, updates := range service.subscribers {
		copy := cloneUpdate(update)
		select {
		case updates <- copy:
		default:
			<-updates
			updates <- copy
		}
	}
}

func stateUpdate(state State) Update {
	return Update{
		TimestampUnixMS:    state.Snapshot.TimestampUnixMS,
		Sequence:           state.Snapshot.Sequence,
		ProcessesTruncated: state.Snapshot.ProcessesTruncated,
		Pressure:           state.Pressure,
		Applications:       compactApplications(state.Analysis.Applications),
		Anomalies:          append([]analysis.Anomaly(nil), state.Anomalies...),
		Demo:               state.Demo,
	}
}

func compactApplications(applications []analysis.Application) []analysis.Application {
	if len(applications) > maximumUpdateApplications {
		applications = applications[:maximumUpdateApplications]
	}
	copy := append([]analysis.Application(nil), applications...)
	for index := range copy {
		copy[index].PIDs = nil
	}
	return copy
}

func anomalyApplicationKeys(applications []analysis.Application) []string {
	if len(applications) > maximumUpdateApplications {
		applications = applications[:maximumUpdateApplications]
	}
	keys := make([]string, len(applications))
	for index := range applications {
		keys[index] = applications[index].Key
	}
	return keys
}

func cloneUpdate(update Update) Update {
	copy := update
	copy.Pressure.Evidence = append([]string(nil), update.Pressure.Evidence...)
	copy.Applications = append([]analysis.Application(nil), update.Applications...)
	for index := range copy.Applications {
		copy.Applications[index].PIDs = append([]uint32(nil), update.Applications[index].PIDs...)
	}
	copy.Anomalies = append([]analysis.Anomaly(nil), update.Anomalies...)
	return copy
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
