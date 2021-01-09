package configs

import (
	"fmt"
	"os"

	"github.com/hashicorp/hcl/v2"
)

// LoadConfigFile config
func (p *Parser) LoadConfigFile(path string) (*File, hcl.Diagnostics) {
	return p.loadConfigFile(path, false)
}

// LoadConfigFileOverride is the same as LoadConfigFile except that it relaxes
// certain required attribute constraints in order to interpret the given
// file as an overrides file.
func (p *Parser) LoadConfigFileOverride(path string) (*File, hcl.Diagnostics) {
	return p.loadConfigFile(path, true)
}

func (p *Parser) loadConfigFile(path string, override bool) (*File, hcl.Diagnostics) {
	src, err := p.fs.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, hcl.Diagnostics{
				{
					Severity: hcl.DiagError,
					Summary:  "Configuration file not found",
					Detail:   fmt.Sprintf("The configuration file %s does not exist.", path),
				},
			}
		}

		return nil, hcl.Diagnostics{
			{
				Severity: hcl.DiagError,
				Summary:  "Failed to read configuration",
				Detail:   fmt.Sprintf("Can't read %s: %s.", path, err),
			},
		}
	}

	return p.Parse(path, src)
}
