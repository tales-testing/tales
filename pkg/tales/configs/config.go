package configs

import (
	"github.com/zclconf/go-cty/cty"
)

// Config struct
type Config struct {
	Name  string    `hcl:"name,label"`
	Value cty.Value `hcl:"value,attr"`
}
