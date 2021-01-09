package configs

import (
	"fmt"

	"github.com/hashicorp/hcl/v2"
	"github.com/zclconf/go-cty/cty"
)

// Module is a container for a set of configuration constructs that are
// evaluated within a common namespace.
type Module struct {
	// SourceDir is the filesystem directory that the module was loaded from.
	//
	// This is populated automatically only for configurations loaded with
	// LoadConfigDir. If the parser is using a virtual filesystem then the
	// path here will be in terms of that virtual filesystem.

	// Any other caller that constructs a module directly with NewModule may
	// assign a suitable value to this attribute before using it for other
	// purposes. It should be treated as immutable by all consumers of Module
	// values.
	SourceDir string

	Configs map[string]cty.Value

	Scenarios map[string]*Scenario

	Keywords map[string]*Keyword
}

// NewModule takes a list of primary files and a list of override files and
// produces a *Module by combining the files together.
//
// If there are any conflicting declarations in the given files -- for example,
// if the same variable name is defined twice -- then the resulting module
// will be incomplete and error diagnostics will be returned. Careful static
// analysis of the returned Module is still possible in this case, but the
// module will probably not be semantically valid.
func NewModule(primaryFiles, overrideFiles []*File) (*Module, hcl.Diagnostics) {
	var diags hcl.Diagnostics
	mod := &Module{
		Configs:   map[string]cty.Value{},
		Scenarios: map[string]*Scenario{},
		Keywords:  map[string]*Keyword{},
	}

	for _, file := range primaryFiles {
		fileDiags := mod.appendFile(file)
		diags = append(diags, fileDiags...)
	}

	for _, file := range overrideFiles {
		fileDiags := mod.mergeFile(file)
		diags = append(diags, fileDiags...)
	}

	return mod, diags
}

func (m *Module) appendFile(file *File) hcl.Diagnostics {
	var diags hcl.Diagnostics
	/*
		for _, v := range file.Configs {
			if existing, exists := m.Configs[v.Name]; exists {
				diags = append(diags, &hcl.Diagnostic{
					Severity: hcl.DiagError,
					Summary:  "Duplicate config declaration",
					Detail:   fmt.Sprintf("A config named %q was already declared at %s. Config names must be unique within a module.", existing.Name, existing.DeclRange),
					Subject:  &v.DeclRange,
				})
			}
			m.Configs[v.Name] = v
		}
	*/

	for _, r := range file.Scenarios {
		key := r.Name

		// key := r.moduleUniqueKey()
		if existing, exists := m.Scenarios[key]; exists {
			diags = append(diags, &hcl.Diagnostic{
				Severity: hcl.DiagError,
				Summary:  fmt.Sprintf("Duplicate scenario %q configuration", existing.Name),
				Detail:   fmt.Sprintf("A scenario named %q was already declared. Scenario names must be unique per type in each module.", existing.Name),
				//Subject:  &r.DeclRange,
			})
			continue
		}

		m.Scenarios[key] = r
	}

	for _, r := range file.Keywords {
		key := r.Name

		// key := r.moduleUniqueKey()
		if existing, exists := m.Keywords[key]; exists {
			diags = append(diags, &hcl.Diagnostic{
				Severity: hcl.DiagError,
				Summary:  fmt.Sprintf("Duplicate keyword %q configuration", existing.Name),
				Detail:   fmt.Sprintf("A keyword named %q was already declared. Keyword names must be unique per type in each module.", existing.Name),
				//Subject:  &r.DeclRange,
			})
			continue
		}

		m.Keywords[key] = r
	}

	return diags
}

func (m *Module) mergeFile(file *File) hcl.Diagnostics {
	var diags hcl.Diagnostics
	/*
		for _, v := range file.Configs {
			existing, exists := m.Configs[v.Name]
			if !exists {
				diags = append(diags, &hcl.Diagnostic{
					Severity: hcl.DiagError,
					Summary:  "Missing base config declaration to override",
					Detail:   fmt.Sprintf("There is no config named %q. An override file can only override a config that was already declared in a primary configuration file.", v.Name),
					Subject:  &v.DeclRange,
				})
				continue
			}
			mergeDiags := existing.merge(v)
			diags = append(diags, mergeDiags...)
		}
	*/

	return diags
}
