package model

// Keyword is parsed and validated, runtime support is intentionally minimal for now.
type Keyword struct {
	Name    string
	File    string
	Inputs  map[string]Expression
	Steps   []*Step
	Outputs map[string]Expression
}
