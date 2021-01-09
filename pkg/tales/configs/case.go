package configs

import (
	"github.com/hashicorp/hcl/v2"
)

// Case struct
type Case struct {
	Type string   `hcl:",label"`
	Name string   `hcl:"name,label"`
	HCL  hcl.Body `hcl:",remain"`
}
