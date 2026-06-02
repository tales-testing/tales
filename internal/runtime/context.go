package runtime

import (
	"sync"

	"github.com/zclconf/go-cty/cty"
)

// ScenarioState stores mutable runtime values for one scenario.
type ScenarioState struct {
	mu      sync.RWMutex
	results map[string]cty.Value
	seed    int64
}

// NewScenarioState creates state with known step keys pre-filled as empty
// objects. seed is the suite seed, carried here so providers that need
// deterministic derived values (e.g. the mail Message-ID) can reach it without
// threading it through the whole execution call chain.
func NewScenarioState(stepNames []string, seed int64) *ScenarioState {
	results := make(map[string]cty.Value, len(stepNames))
	for _, name := range stepNames {
		results[name] = cty.EmptyObjectVal
	}

	return &ScenarioState{results: results, seed: seed}
}

// Seed returns the suite seed for deterministic derivations.
func (s *ScenarioState) Seed() int64 {
	return s.seed
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
