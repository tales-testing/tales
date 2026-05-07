package runtime

import (
	"sync"

	"github.com/zclconf/go-cty/cty"
)

// ScenarioState stores mutable runtime values for one scenario.
type ScenarioState struct {
	mu      sync.RWMutex
	results map[string]cty.Value
}

// NewScenarioState creates state with known step keys pre-filled as empty objects.
func NewScenarioState(stepNames []string) *ScenarioState {
	results := make(map[string]cty.Value, len(stepNames))
	for _, name := range stepNames {
		results[name] = cty.EmptyObjectVal
	}

	return &ScenarioState{results: results}
}

// GetResultMap clones result map.
func (s *ScenarioState) GetResultMap() map[string]cty.Value {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make(map[string]cty.Value, len(s.results))
	for k, v := range s.results {
		out[k] = v
	}

	return out
}

// SetStepResult updates one step result.
func (s *ScenarioState) SetStepResult(step string, value cty.Value) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.results[step] = value
}
