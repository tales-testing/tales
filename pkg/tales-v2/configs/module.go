package configs

import (
	"fmt"

	"github.com/hashicorp/hcl/v2"
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

	Variables map[string]*Variable
	Outputs   map[string]*Output

	Scenarios map[string]*Scenario
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
		Variables: map[string]*Variable{},
		Outputs:   map[string]*Output{},
		Scenarios: map[string]*Scenario{},
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

	for _, v := range file.Variables {
		if existing, exists := m.Variables[v.Name]; exists {
			diags = append(diags, &hcl.Diagnostic{
				Severity: hcl.DiagError,
				Summary:  "Duplicate variable declaration",
				Detail:   fmt.Sprintf("A variable named %q was already declared at %s. Variable names must be unique within a module.", existing.Name, existing.DeclRange),
				Subject:  &v.DeclRange,
			})
		}
		m.Variables[v.Name] = v
	}

	for _, o := range file.Outputs {
		if existing, exists := m.Outputs[o.Name]; exists {
			diags = append(diags, &hcl.Diagnostic{
				Severity: hcl.DiagError,
				Summary:  "Duplicate output definition",
				Detail:   fmt.Sprintf("An output named %q was already defined at %s. Output names must be unique within a module.", existing.Name, existing.DeclRange),
				Subject:  &o.DeclRange,
			})
		}
		m.Outputs[o.Name] = o
	}

	for _, r := range file.Scenarios {
		key := r.moduleUniqueKey()
		if existing, exists := m.Scenarios[key]; exists {
			diags = append(diags, &hcl.Diagnostic{
				Severity: hcl.DiagError,
				Summary:  fmt.Sprintf("Duplicate scenario %q configuration", existing.Name),
				Detail:   fmt.Sprintf("A scenario named %q was already declared at %s. Scenario names must be unique per type in each module.", existing.Name, existing.DeclRange),
				Subject:  &r.DeclRange,
			})
			continue
		}
		m.Scenarios[key] = r
	}

	return diags
}

func (m *Module) mergeFile(file *File) hcl.Diagnostics {
	var diags hcl.Diagnostics

	for _, v := range file.Variables {
		existing, exists := m.Variables[v.Name]
		if !exists {
			diags = append(diags, &hcl.Diagnostic{
				Severity: hcl.DiagError,
				Summary:  "Missing base variable declaration to override",
				Detail:   fmt.Sprintf("There is no variable named %q. An override file can only override a variable that was already declared in a primary configuration file.", v.Name),
				Subject:  &v.DeclRange,
			})
			continue
		}
		mergeDiags := existing.merge(v)
		diags = append(diags, mergeDiags...)
	}

	for _, o := range file.Outputs {
		existing, exists := m.Outputs[o.Name]
		if !exists {
			diags = append(diags, &hcl.Diagnostic{
				Severity: hcl.DiagError,
				Summary:  "Missing base output definition to override",
				Detail:   fmt.Sprintf("There is no output named %q. An override file can only override an output that was already defined in a primary configuration file.", o.Name),
				Subject:  &o.DeclRange,
			})
			continue
		}
		mergeDiags := existing.merge(o)
		diags = append(diags, mergeDiags...)
	}

	return diags
}
