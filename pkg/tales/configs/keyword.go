package configs

import (
	"github.com/hashicorp/hcl/v2"
	"github.com/zclconf/go-cty/cty"
)

// KeywordOutput struct
type KeywordOutput struct {
	Outputs []*Output `hcl:"output,block"`
}

// Keyword struct
type Keyword struct {
	Name       string `hcl:"name,label"`
	Args       []*Arg `hcl:"arg,block"`
	CaseConfig *Case  `hcl:"case,block"`
	Case       TestCase
	Outputs    []*Output
	HCL        hcl.Body `hcl:",remain"`
}

// Arg struct
type Arg struct {
	Name    string `hcl:"name,label"`
	Default cty.Value
	Value   cty.Value `hcl:"value,optional"`
	HCL     hcl.Body  `hcl:",remain"`
}

// ArgDefault struct
type ArgDefault struct {
	Default cty.Value `hcl:"default,optional"`
}

// Output struct
type Output struct {
	Name  string    `hcl:"name,label"`
	Value cty.Value `hcl:"value,attr"`
}
