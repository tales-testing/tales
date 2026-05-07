package model

// Suite is the merged representation of one or more .tales files.
type Suite struct {
	Version    int
	Files      []string
	ConfigExpr map[string]Expression
	Generators map[string]*Generator
	Keywords   map[string]*Keyword
	Scenarios  []*Scenario
}
