package model

// Suite is the merged representation of one or more .tales files.
type Suite struct {
	Version    int
	Files      []string
	ConfigExpr map[string]Expression
	Generators map[string]*Generator
	Keywords   map[string]*Keyword
	Scenarios  []*Scenario
	// Teardown holds the suite-level teardown steps, executed once after
	// every scenario has finished and before providers are closed. Empty
	// when no file declares a top-level teardown block.
	Teardown []*Step
	// TeardownFile is the .tales file that declared the suite-level teardown.
	// Only one file may declare it, so this doubles as the "already claimed"
	// marker used to build the duplicate-declaration diagnostic.
	TeardownFile string
}
