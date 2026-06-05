package runtime

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/tales-testing/tales/internal/lang"
	"github.com/tales-testing/tales/internal/model"
	"github.com/tales-testing/tales/internal/provider"
	"github.com/tales-testing/tales/internal/report"
	"github.com/tales-testing/tales/internal/workspace"
	"github.com/zclconf/go-cty/cty"
)

// exprPathSaveBody labels the save.body expression in diagnostics and error
// paths.
const exprPathSaveBody = "save.body"

// applySaveAndDownload writes the HTTP response body to the path declared in
// the step's save block (resolved under scenario.workdir) and injects a
// response.download object carrying the file path, size and hex digests. The
// body is taken from response.body, which is the byte-exact string the HTTP
// provider built from the raw response, so binary payloads round-trip without
// corruption. On any failure it returns a kindSave error detail and writes
// nothing further. download is added to output.Response so expect and capture
// see response.download.*.
func (r *Runner) applySaveAndDownload(evaluator *lang.Evaluator, scope *lang.ScopeData, scenarioName string, state *ScenarioState, step *model.Step, output *provider.Output) *report.ErrorDetail {
	bodyVal, ok := output.Response["body"]
	if !ok || bodyVal.IsNull() || bodyVal.Type() != cty.String {
		return &report.ErrorDetail{Kind: kindSave, Path: exprPathSaveBody, Message: "save requires an HTTP response body"}
	}

	data := []byte(bodyVal.AsString())

	rawPath, detail := evalSaveBodyPath(evaluator, *scope, scenarioName, step)
	if detail != nil {
		return detail
	}

	resolver := workspace.Resolver{Workdir: state.Workdir(), ProjectDir: r.projectDir}

	target, err := resolver.ResolveOutput(rawPath)
	if err != nil {
		return &report.ErrorDetail{Kind: kindSave, Path: exprPathSaveBody, Message: err.Error()}
	}

	if err := writeSaveFile(target, data); err != nil {
		return &report.ErrorDetail{Kind: kindSave, Path: exprPathSaveBody, Message: err.Error()}
	}

	download, err := buildDownloadMeta(target, data)
	if err != nil {
		return &report.ErrorDetail{Kind: kindSave, Path: exprPathSaveBody, Message: err.Error()}
	}

	output.Response["download"] = download
	scope.Response = output.Response

	return nil
}

// evalSaveBodyPath evaluates the save.body expression to a string path.
func evalSaveBodyPath(evaluator *lang.Evaluator, scope lang.ScopeData, scenarioName string, step *model.Step) (string, *report.ErrorDetail) {
	value, err := evaluator.Eval(step.Save.Body, scope, lang.GenerateMeta{Scenario: scenarioName, Step: step.Name, ExprPath: exprPathSaveBody})
	if err != nil {
		return "", &report.ErrorDetail{Kind: kindSave, Path: exprPathSaveBody, Message: err.Error()}
	}

	if value.IsNull() || value.Type() != cty.String {
		return "", &report.ErrorDetail{Kind: kindSave, Path: exprPathSaveBody, Message: "save.body must evaluate to a string path"}
	}

	return value.AsString(), nil
}

// writeSaveFile creates the parent directory and writes data to target. The
// file is owner-only (0o600); the directory is 0o755 like the workspace root.
func writeSaveFile(target string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("create save directory: %w", err)
	}

	if err := os.WriteFile(target, data, 0o600); err != nil {
		return fmt.Errorf("write save file: %w", err)
	}

	return nil
}

// buildDownloadMeta computes the response.download object: the saved path, the
// byte length, and a hex digest for every supported algorithm.
func buildDownloadMeta(path string, data []byte) (cty.Value, error) {
	attrs := map[string]cty.Value{
		keyPath: cty.StringVal(path),
		keySize: cty.NumberIntVal(int64(len(data))),
	}

	for _, algo := range lang.HashAlgorithms() {
		digest, err := lang.HashHex(algo, data)
		if err != nil {
			return cty.NilVal, fmt.Errorf("hash %s: %w", algo, err)
		}

		attrs[algo] = cty.StringVal(digest)
	}

	return cty.ObjectVal(attrs), nil
}
