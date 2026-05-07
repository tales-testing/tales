package parser

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/hyperxlab/tales/internal/model"
)

// LoadPath loads one .tales file or all .tales files recursively from a directory.
func LoadPath(path string) (*model.Suite, hcl.Diagnostics) {
	stat, err := os.Stat(path)
	if err != nil {
		return nil, hcl.Diagnostics{diagError("Invalid path", fmt.Sprintf("Cannot open %q: %v", path, err), nil)}
	}

	files := make([]string, 0)
	if stat.IsDir() {
		walkErr := filepath.Walk(path, func(current string, info os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if info.IsDir() {
				return nil
			}
			if strings.HasSuffix(info.Name(), ".tales") {
				files = append(files, current)
			}
			return nil
		})
		if walkErr != nil {
			return nil, hcl.Diagnostics{diagError("Read error", fmt.Sprintf("Cannot walk %q: %v", path, walkErr), nil)}
		}
	} else {
		if !strings.HasSuffix(path, ".tales") {
			return nil, hcl.Diagnostics{diagError("Invalid file", "Input file must end with .tales", nil)}
		}
		files = append(files, path)
	}

	sort.Strings(files)
	if len(files) == 0 {
		return nil, hcl.Diagnostics{diagError("No tales files", fmt.Sprintf("No .tales files found under %q", path), nil)}
	}

	suite := &model.Suite{
		Version:    1,
		Files:      files,
		ConfigExpr: map[string]model.Expression{},
		Generators: map[string]*model.Generator{},
		Keywords:   map[string]*model.Keyword{},
		Scenarios:  make([]*model.Scenario, 0),
	}

	parser := hclparse.NewParser()
	var diags hcl.Diagnostics

	for _, file := range files {
		hclFile, parseDiags := parser.ParseHCLFile(file)
		diags = append(diags, parseDiags...)
		if parseDiags.HasErrors() {
			continue
		}

		decoded, decodeDiags := decodeFile(file, hclFile.Body)
		diags = append(diags, decodeDiags...)
		if decodeDiags.HasErrors() {
			continue
		}

		mergeSuite(suite, decoded, &diags)
	}

	validateSuite(suite, &diags)

	if diags.HasErrors() {
		return nil, diags
	}

	return suite, diags
}

func mergeSuite(dst, src *model.Suite, diags *hcl.Diagnostics) {
	if src.Version > 0 {
		dst.Version = src.Version
	}
	for k, v := range src.ConfigExpr {
		dst.ConfigExpr[k] = v
	}
	for name, gen := range src.Generators {
		if _, exists := dst.Generators[name]; exists {
			*diags = append(*diags, diagError("Duplicate generator", fmt.Sprintf("Generator %q is defined multiple times", name), nil))
			continue
		}
		dst.Generators[name] = gen
	}
	for name, kw := range src.Keywords {
		if _, exists := dst.Keywords[name]; exists {
			*diags = append(*diags, diagError("Duplicate keyword", fmt.Sprintf("Keyword %q is defined multiple times", name), nil))
			continue
		}
		dst.Keywords[name] = kw
	}
	dst.Scenarios = append(dst.Scenarios, src.Scenarios...)
}

func validateSuite(suite *model.Suite, diags *hcl.Diagnostics) {
	scenarioNames := map[string]struct{}{}
	for _, sc := range suite.Scenarios {
		if _, exists := scenarioNames[sc.Name]; exists {
			*diags = append(*diags, diagError("Duplicate scenario", fmt.Sprintf("Scenario %q is defined multiple times", sc.Name), nil))
		}
		scenarioNames[sc.Name] = struct{}{}

		stepNames := map[string]struct{}{}
		for _, step := range sc.Steps {
			if _, exists := stepNames[step.Name]; exists {
				*diags = append(*diags, diagError("Duplicate step", fmt.Sprintf("Scenario %q has duplicate step %q", sc.Name, step.Name), nil))
			}
			stepNames[step.Name] = struct{}{}
		}
		for _, step := range sc.Teardown {
			if _, exists := stepNames[step.Name]; exists {
				*diags = append(*diags, diagError("Duplicate step", fmt.Sprintf("Scenario %q duplicates step/teardown name %q", sc.Name, step.Name), nil))
			}
			stepNames[step.Name] = struct{}{}
		}
	}
}

func diagError(summary, detail string, subject *hcl.Range) *hcl.Diagnostic {
	return &hcl.Diagnostic{
		Severity: hcl.DiagError,
		Summary:  summary,
		Detail:   detail,
		Subject:  subject,
	}
}
