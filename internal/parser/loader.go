package parser

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/tales-testing/tales/internal/lang"
	"github.com/tales-testing/tales/internal/model"
)

// LoadPath loads one .tales file or all .tales files recursively from a directory.
func LoadPath(path string) (*model.Suite, hcl.Diagnostics) {
	stat, err := os.Stat(path)
	if err != nil {
		return nil, hcl.Diagnostics{diagError("Invalid path", fmt.Sprintf("Cannot open %q: %v", path, err), nil)}
	}

	files, fileErr := collectFiles(path, stat)
	if fileErr != nil {
		return nil, hcl.Diagnostics{diagError("Read error", fileErr.Error(), nil)}
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

func collectFiles(path string, stat os.FileInfo) ([]string, error) {
	if !stat.IsDir() {
		if !strings.HasSuffix(path, ".tales") {
			return nil, fmt.Errorf("input file must end with .tales")
		}

		return []string{path}, nil
	}

	files := make([]string, 0)

	walkErr := filepath.Walk(path, func(current string, info os.FileInfo, err error) error {
		if err != nil {
			return err
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
		return nil, fmt.Errorf("cannot walk %q: %w", path, walkErr)
	}

	return files, nil
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

		validateScenarioStepOrder(sc, diags)
		validateScenarioStepVars(sc, diags)
	}

	validateKeywordStepNames(suite, diags)
	validateKeywordStepVars(suite, diags)
}

// validateScenarioStepVars enforces the step-local vars contract for every
// step and teardown step in the scenario.
func validateScenarioStepVars(sc *model.Scenario, diags *hcl.Diagnostics) {
	for _, step := range sc.Steps {
		if err := lang.ValidateStepVars(step); err != nil {
			*diags = append(*diags, diagError("Invalid step vars", fmt.Sprintf("Scenario %q: %v", sc.Name, err), nil))
		}
	}

	for _, step := range sc.Teardown {
		if err := lang.ValidateStepVars(step); err != nil {
			*diags = append(*diags, diagError("Invalid step vars", fmt.Sprintf("Scenario %q teardown: %v", sc.Name, err), nil))
		}
	}
}

// validateKeywordStepVars enforces the step-local vars contract for keyword
// sub-steps as well.
func validateKeywordStepVars(suite *model.Suite, diags *hcl.Diagnostics) {
	names := make([]string, 0, len(suite.Keywords))
	for name := range suite.Keywords {
		names = append(names, name)
	}

	sort.Strings(names)

	for _, name := range names {
		for _, step := range suite.Keywords[name].Steps {
			if err := lang.ValidateStepVars(step); err != nil {
				*diags = append(*diags, diagError("Invalid step vars", fmt.Sprintf("Keyword %q: %v", name, err), nil))
			}
		}
	}
}

// validateKeywordStepNames rejects duplicate step names within a keyword.
// Unlike scenario steps, keyword sub-steps were previously unchecked; the
// sequential runner and the source-order reordering both assume unique
// (provider, name) keys inside a keyword body.
func validateKeywordStepNames(suite *model.Suite, diags *hcl.Diagnostics) {
	names := make([]string, 0, len(suite.Keywords))
	for name := range suite.Keywords {
		names = append(names, name)
	}

	sort.Strings(names)

	for _, name := range names {
		stepNames := map[string]struct{}{}

		for _, step := range suite.Keywords[name].Steps {
			if _, exists := stepNames[step.Name]; exists {
				*diags = append(*diags, diagError("Duplicate step", fmt.Sprintf("Keyword %q has duplicate step %q", name, step.Name), nil))
			}

			stepNames[step.Name] = struct{}{}
		}
	}
}

// validateScenarioStepOrder rejects steps that reference an unknown step or a
// step defined later in the file: with sequential file-order execution a step
// can only consume results produced by an earlier step.
//
// Teardown steps are intentionally not checked. They run after every main
// step and routinely guard optional references with when = can(...), so a
// reference to a step that never produced a result is a legitimate pattern
// there rather than an error.
func validateScenarioStepOrder(sc *model.Scenario, diags *hcl.Diagnostics) {
	if err := lang.ValidateStepOrder(sc.Steps, nil); err != nil {
		*diags = append(*diags, diagError("Invalid step order", fmt.Sprintf("Scenario %q: %v", sc.Name, err), nil))
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
