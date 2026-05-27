package keyword

import (
	"context"
	"fmt"

	"github.com/tales-testing/tales/internal/provider"
)

// Provider is reserved for future keyword execution.
type Provider struct{}

// New creates keyword provider.
func New() *Provider {
	return &Provider{}
}

// Type returns provider type.
func (p *Provider) Type() string {
	return "keyword"
}

// Execute currently returns clear unsupported error.
func (p *Provider) Execute(ctx context.Context, input provider.Input) (*provider.Output, error) {
	_ = ctx
	_ = input

	return nil, fmt.Errorf("keyword provider is not implemented yet")
}
