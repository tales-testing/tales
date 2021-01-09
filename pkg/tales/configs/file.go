package configs

// File struct
type File struct {
	Version   int         `hcl:"version"`
	Scenarios []*Scenario `hcl:"scenario,block"`
	Keywords  []*Keyword  `hcl:"keyword,block"`
	Configs   []*Config   `hcl:"config,block"`
}
