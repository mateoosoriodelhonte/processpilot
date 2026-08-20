package ai

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/mateoosoriodelhonte/processpilot/internal/analysis"
)

type Input struct {
	Application         string
	Category            analysis.Category
	Risk                analysis.Risk
	CPUPercent          float64
	MemoryBytes         uint64
	TotalMemoryBytes    uint64
	DeterministicReason string
	PotentialImpact     string
}

type Explanation struct {
	Text          string `json:"text"`
	Provider      string `json:"provider"`
	GeneratedByAI bool   `json:"generatedByAI"`
}

type Provider interface {
	Explain(context.Context, Input) (Explanation, error)
}

// FallbackProvider keeps explanations available when an optional local model is
// stopped or unreachable. Classification and analysis never depend on either provider.
type FallbackProvider struct {
	Primary  Provider
	Fallback Provider
}

func (provider FallbackProvider) Explain(ctx context.Context, input Input) (Explanation, error) {
	if provider.Primary != nil {
		if explanation, err := provider.Primary.Explain(ctx, input); err == nil {
			return explanation, nil
		}
	}
	if provider.Fallback == nil {
		return Explanation{}, errors.New("explanation provider is unavailable")
	}
	return provider.Fallback.Explain(ctx, input)
}

type NoAIProvider struct{}

func (NoAIProvider) Explain(_ context.Context, input Input) (Explanation, error) {
	if err := input.validate(); err != nil {
		return Explanation{}, err
	}
	memory := fmt.Sprintf("%.2f GiB", float64(input.MemoryBytes)/float64(uint64(1)<<30))
	if input.TotalMemoryBytes > 0 {
		memory += fmt.Sprintf(" (%.1f%% of physical memory)", float64(input.MemoryBytes)/float64(input.TotalMemoryBytes)*100)
	}
	category := strings.ReplaceAll(strings.ToLower(string(input.Category)), "_", " ")
	text := fmt.Sprintf(
		"%s is classified as %s. It is using %s and %.1f%% CPU. %s %s",
		input.Application, category, memory, input.CPUPercent,
		input.DeterministicReason, input.PotentialImpact,
	)
	return Explanation{Text: text, Provider: "ProcessPilot", GeneratedByAI: false}, nil
}

func (input Input) validate() error {
	if strings.TrimSpace(input.Application) == "" || len(input.Application) > 256 {
		return errors.New("explanation application is empty or too long")
	}
	if input.Category == "" || input.Risk == "" {
		return errors.New("explanation classification is required")
	}
	if math.IsNaN(input.CPUPercent) || math.IsInf(input.CPUPercent, 0) || input.CPUPercent < 0 || input.CPUPercent > 102_400 {
		return errors.New("explanation CPU value is invalid")
	}
	if input.TotalMemoryBytes > 0 && input.MemoryBytes > input.TotalMemoryBytes {
		return errors.New("explanation memory exceeds physical memory")
	}
	if len(input.DeterministicReason) > 1_000 || len(input.PotentialImpact) > 1_000 {
		return errors.New("explanation classification text is too long")
	}
	return nil
}
