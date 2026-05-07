package model

// Scenario defines executable test flow.
type Scenario struct {
	Name     string
	Tags     []string
	File     string
	Steps    []*Step
	Teardown []*Step
}
