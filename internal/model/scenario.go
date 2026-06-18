package model

// Scenario defines executable test flow.
type Scenario struct {
	Name      string
	Tags      []string
	File      string
	Steps     []*Step
	Teardown  []*Step
	SkipRules []SkipRule
	// Record, when non-nil, instructs scenario hooks to capture a screen
	// recording for the whole scenario. V1 is handled by the mobile (iOS)
	// provider; other providers ignore it.
	Record *ScenarioRecord
}
