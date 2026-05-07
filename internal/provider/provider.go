package provider

import (
	"context"
	"time"

	"github.com/hyperxlab/tales/internal/model"
	"github.com/zclconf/go-cty/cty"
)

// Input is provider execution input.
type Input struct {
	Scenario string
	Step     *model.Step
	Request  map[string]cty.Value
	Expect   map[string]cty.Value
	Timeout  time.Duration
}

// Output is provider execution output.
type Output struct {
	Request    map[string]cty.Value
	Response   map[string]cty.Value
	Duration   time.Duration
	StatusCode int
}

// Provider executes one step.
type Provider interface {
	Type() string
	Execute(ctx context.Context, input Input) (*Output, error)
}

// Registry maps provider type to implementation.
type Registry struct {
	items map[string]Provider
}

// NewRegistry creates registry.
func NewRegistry(providers ...Provider) *Registry {
	items := make(map[string]Provider, len(providers))
	for _, p := range providers {
		items[p.Type()] = p
	}
	return &Registry{items: items}
}

// Get provider by type.
func (r *Registry) Get(providerType string) (Provider, bool) {
	p, ok := r.items[providerType]
	return p, ok
}
