package analysis

import (
	"fmt"
	"sort"

	"github.com/mateoosoriodelhonte/processpilot/internal/protocol"
)

type OwnershipConfidence string

const (
	ConfidenceKnownSignature    OwnershipConfidence = "KNOWN_SIGNATURE"
	ConfidenceApplicationBundle OwnershipConfidence = "APPLICATION_BUNDLE"
	ConfidenceUnknown           OwnershipConfidence = "UNKNOWN"
)

type Ownership struct {
	Application string              `json:"application"`
	OwnerPID    uint32              `json:"ownerPid"`
	Chain       []uint32            `json:"chain"`
	Confidence  OwnershipConfidence `json:"confidence"`
}

type Process struct {
	Observed       protocol.ProcessSample `json:"observed"`
	Classification Classification         `json:"classification"`
	Ownership      Ownership              `json:"ownership"`
}

type Application struct {
	Name         string   `json:"name"`
	Category     Category `json:"category"`
	Risk         Risk     `json:"stoppingRisk"`
	ProcessCount int      `json:"processCount"`
	CPUPercent   float64  `json:"cpuPercent"`
	MemoryBytes  uint64   `json:"memoryBytes"`
	PIDs         []uint32 `json:"pids"`
}

type Result struct {
	Processes    []Process     `json:"processes"`
	Applications []Application `json:"applications"`
}

func Build(samples []protocol.ProcessSample) Result {
	byPID := make(map[uint32]protocol.ProcessSample, len(samples))
	classifications := make(map[uint32]Classification, len(samples))
	for _, sample := range samples {
		byPID[sample.PID] = sample
		classifications[sample.PID] = Classify(sample)
	}

	result := Result{Processes: make([]Process, 0, len(samples))}
	groups := make(map[string]*Application)
	for _, sample := range samples {
		classified := classifications[sample.PID]
		ownership := resolveOwnership(sample.PID, byPID, classifications)
		result.Processes = append(result.Processes, Process{
			Observed: sample, Classification: classified, Ownership: ownership,
		})

		groupKey := ownership.Application
		if ownership.Confidence == ConfidenceUnknown {
			groupKey = fmt.Sprintf("unknown:%d", sample.PID)
		}
		group := groups[groupKey]
		if group == nil {
			group = &Application{Name: ownership.Application, Category: classified.Category, Risk: classified.Risk}
			groups[groupKey] = group
		}
		group.ProcessCount++
		group.CPUPercent += sample.CPUPercent
		group.MemoryBytes += sample.MemoryBytes
		group.PIDs = append(group.PIDs, sample.PID)
		group.Category = mergeCategory(group.Category, classified.Category)
		group.Risk = moreConservativeRisk(group.Risk, classified.Risk)
	}

	sort.Slice(result.Processes, func(i, j int) bool {
		return result.Processes[i].Observed.PID < result.Processes[j].Observed.PID
	})
	for _, group := range groups {
		sort.Slice(group.PIDs, func(i, j int) bool { return group.PIDs[i] < group.PIDs[j] })
		result.Applications = append(result.Applications, *group)
	}
	sort.Slice(result.Applications, func(i, j int) bool {
		if result.Applications[i].MemoryBytes == result.Applications[j].MemoryBytes {
			return result.Applications[i].Name < result.Applications[j].Name
		}
		return result.Applications[i].MemoryBytes > result.Applications[j].MemoryBytes
	})
	return result
}

func resolveOwnership(pid uint32, samples map[uint32]protocol.ProcessSample, classifications map[uint32]Classification) Ownership {
	chain := make([]uint32, 0, 8)
	seen := make(map[uint32]struct{})
	current := pid
	var bestPID uint32
	var best Classification

	for len(chain) < 64 {
		if _, exists := seen[current]; exists {
			break
		}
		seen[current] = struct{}{}
		chain = append(chain, current)
		sample, exists := samples[current]
		if !exists {
			break
		}
		classified := classifications[current]
		if classified.Category != CategoryUnknown && bestPID == 0 {
			bestPID, best = current, classified
		}
		if sample.ParentPID == nil {
			break
		}
		current = *sample.ParentPID
	}

	if bestPID != 0 {
		confidence := ConfidenceKnownSignature
		if best.Category == CategoryUserApplication {
			confidence = ConfidenceApplicationBundle
		}
		return Ownership{Application: best.Application, OwnerPID: bestPID, Chain: chain, Confidence: confidence}
	}
	sample := samples[pid]
	return Ownership{Application: sample.Name, OwnerPID: pid, Chain: chain, Confidence: ConfidenceUnknown}
}

func mergeCategory(current, candidate Category) Category {
	if current == candidate {
		return current
	}
	if (current == CategoryBrowser && candidate == CategoryBrowserHelper) || (current == CategoryBrowserHelper && candidate == CategoryBrowser) {
		return CategoryBrowser
	}
	if current == CategoryUnknown {
		return candidate
	}
	if candidate == CategoryUnknown {
		return current
	}
	return CategoryUnknown
}

func moreConservativeRisk(current, candidate Risk) Risk {
	rank := map[Risk]int{RiskLow: 1, RiskMedium: 2, RiskUnknown: 3, RiskHigh: 4}
	if rank[candidate] > rank[current] {
		return candidate
	}
	return current
}
