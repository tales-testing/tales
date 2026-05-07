package model

// Generator declaration from DSL.
type Generator struct {
	Type   string
	Name   string
	File   string
	Params map[string]Expression
}
