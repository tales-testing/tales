package provider

import (
	"context"
	"time"

	"github.com/tales-testing/tales/internal/model"
	"github.com/zclconf/go-cty/cty"
)

// Input is provider execution input.
type Input struct {
	Scenario string
	Step     *model.Step
	Phase    string
	Attempt  int
	Config   map[string]cty.Value
	Request  map[string]cty.Value
	Expect   map[string]cty.Value
	Mobile   *MobileExecution
	SQL      *SQLExecution
	Browser  *BrowserExecution
	Timeout  time.Duration
}

// SQLExecution carries the resolved data for one sql step ready to be
// executed by the SQL provider. The runner evaluates step expressions into
// these concrete Go values before invoking the provider.
type SQLExecution struct {
	Connection string
	Mode       string // "exec" or "query"
	SQL        string
	Args       []any
}

// Output is provider execution output.
type Output struct {
	Request       map[string]cty.Value
	Response      map[string]cty.Value
	Duration      time.Duration
	StatusCode    int
	ActionResults []ActionResult
}

// ActionResult is a provider-agnostic record of one UI action executed
// within a step. The runtime converts it into a report.ActionResult after
// the provider returns. Providers that do not emit actions leave this slice
// nil; HTTP and keyword providers are unaffected.
//
// Secure actions MUST carry Value == "***" — providers mask before
// constructing the result.
type ActionResult struct {
	Index      int
	Kind       string
	Label      string
	SelectorID string
	Secure     bool
	Value      string
	Status     string
	Duration   time.Duration
	Screenshot string
	Hierarchy  string
	Err        error
	StartedAt  time.Time
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

// All returns every registered provider. The order is not stable.
func (r *Registry) All() []Provider {
	out := make([]Provider, 0, len(r.items))
	for _, p := range r.items {
		out = append(out, p)
	}

	return out
}
