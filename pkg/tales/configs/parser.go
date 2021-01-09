package configs

import (
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/spf13/afero"
	"github.com/zclconf/go-cty/cty"
)

// Parser is the main interface to read configuration files and other related
// files from disk.
//
// It retains a cache of all files that are loaded so that they can be used
// to create source code snippets in diagnostics, etc.
type Parser struct {
	fs  afero.Afero
	p   *hclparse.Parser
	ctx *hcl.EvalContext
}

// NewParser creates and returns a new Parser that reads files from the given
// filesystem. If a nil filesystem is passed then the system's "real" filesystem
// will be used, via afero.OsFs.
func NewParser(fs afero.Fs) *Parser {
	if fs == nil {
		fs = afero.OsFs{}
	}

	return &Parser{
		fs:  afero.Afero{Fs: fs},
		p:   hclparse.NewParser(),
		ctx: createEvalContext(),
	}
}

// Context return EvalContext
func (p *Parser) Context() *hcl.EvalContext {
	return p.ctx
}

// Parse config
func (p *Parser) Parse(filename string, src []byte) (*File, hcl.Diagnostics) {
	f, diags := hclsyntax.ParseConfig(src, filename, hcl.Pos{Line: 1, Column: 1})

	if diags.HasErrors() {
		return nil, diags
	}

	file := &File{}

	diags = gohcl.DecodeBody(f.Body, p.ctx, file)

	p.parseConfigVars(file)

	return file, diags
}

func (p *Parser) parseConfigVars(file *File) {
	configs := p.ctx.Variables["config"].AsValueMap()
	if configs == nil {
		configs = map[string]cty.Value{}
	}

	for _, cfg := range file.Configs {
		configs[cfg.Name] = cfg.Value
	}

	p.ctx.Variables["config"] = cty.ObjectVal(configs)
}
