package runtime

import (
	"sync"

	"github.com/zclconf/go-cty/cty"
)

// ScenarioState stores mutable runtime values for one scenario.
type ScenarioState struct {
	mu           sync.RWMutex
	results      map[string]cty.Value
	seed         int64
	workdir      string
	artifactsDir string
}

// NewScenarioState creates state with known step keys pre-filled as empty
// objects. seed is the suite seed, carried here so providers that need
// deterministic derived values (e.g. the mail Message-ID) can reach it without
// threading it through the whole execution call chain. workdir / artifactsDir
// are the scenario workspace roots; the save / file / exec paths resolve
// against them. Both are absolute, created before the scenario's steps run.
func NewScenarioState(stepNames []string, seed int64, workdir, artifactsDir string) *ScenarioState {
	results := make(map[string]cty.Value, len(stepNames))
	for _, name := range stepNames {
		results[name] = cty.EmptyObjectVal
	}

	return &ScenarioState{results: results, seed: seed, workdir: workdir, artifactsDir: artifactsDir}
}

// Seed returns the suite seed for deterministic derivations.
func (s *ScenarioState) Seed() int64 {
	return s.seed
}

// Workdir returns the absolute per-scenario workspace directory. Relative
// save / file / exec paths resolve under it.
func (s *ScenarioState) Workdir() string {
	return s.workdir
}

// ArtifactsDir returns the absolute per-scenario artifacts directory.
func (s *ScenarioState) ArtifactsDir() string {
	return s.artifactsDir
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
