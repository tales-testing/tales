package runtime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/tales-testing/tales/internal/model"
	"github.com/tales-testing/tales/internal/provider"
	"github.com/tales-testing/tales/internal/report"
	"github.com/zclconf/go-cty/cty"
)

// bodyProvider is an http-typed provider returning a fixed (possibly binary)
// body so save / download behavior can be asserted byte-exactly.
type bodyProvider struct {
	body []byte
}

func (p *bodyProvider) Type() string { return "http" }

func (p *bodyProvider) Execute(_ context.Context, input provider.Input) (*provider.Output, error) {
	_ = input

	return &provider.Output{
		StatusCode: 200,
		Request:    input.Request,
		// RawBody mirrors the real HTTP provider: the byte-exact body that
		// save / download must use, distinct from the NFC-normalized
		// Response["body"] cty string.
		RawBody: p.body,
		Response: map[string]cty.Value{
			"status":  cty.NumberIntVal(200),
			"headers": cty.EmptyObjectVal,
			"body":    cty.StringVal(string(p.body)),
			"json":    cty.NullVal(cty.DynamicPseudoType),
		},
	}, nil
}

func runSaveScenario(t *testing.T, step *model.Step) (string, *report.SuiteResult, []byte) {
	t.Helper()

	body := []byte{0x25, 0x50, 0x44, 0x46, 0x00, 0x01, 0x02, 0xff, 0xfe, 0x0a} // %PDF + binary

	base, result := runSaveScenarioWithBody(t, step, body)

	return base, result, body
}

func runSaveScenarioWithBody(t *testing.T, step *model.Step, body []byte) (string, *report.SuiteResult) {
	t.Helper()

	prov := &bodyProvider{body: body}
	runner := NewRunner(provider.NewRegistry(prov))

	base := t.TempDir()

	result, err := runner.Run(context.Background(), &model.Suite{Scenarios: []*model.Scenario{{
		Name:  "download",
		File:  "test.tales",
		Steps: []*model.Step{step},
	}}}, Options{Seed: 1, Parallel: 1, ArtifactsBase: base, ProjectDir: t.TempDir()})
	if err != nil {
		t.Fatalf("run error: %v", err)
	}

	return base, result
}

func newSaveStep() *model.Step {
	step := newHTTPStep("download")
	step.Capture = map[string]model.Expression{
		"path":   expr(`response.download.path`),
		"size":   expr(`response.download.size_bytes`),
		"sha256": expr(`response.download.sha256`),
		"sha512": expr(`response.download.sha512`),
	}
	step.Expect = &model.Expect{Status: expr(`200`)}
	step.Save = &model.SaveBlock{Body: expr(`"downloads/cert.pdf"`)}

	return step
}

func TestSaveWritesExactBytesAndExposesDownload(t *testing.T) {
	t.Parallel()

	step := newSaveStep()

	_, result, body := runSaveScenario(t, step)

	stepResult := result.Scenarios[0].Steps[0]
	if stepResult.Status != report.StatusPass {
		t.Fatalf("step should pass: %#v", stepResult.Failure)
	}

	// Read the saved file back and compare bytes exactly.
	saved := capturedString(t, result, "path")

	onDisk, err := os.ReadFile(saved)
	if err != nil {
		t.Fatalf("read saved file: %v", err)
	}

	if string(onDisk) != string(body) {
		t.Fatalf("saved bytes differ from response body")
	}

	if filepath.Base(saved) != "cert.pdf" {
		t.Fatalf("unexpected saved path %q", saved)
	}

	wantSHA256 := sha256.Sum256(body)
	if got := capturedString(t, result, "sha256"); got != hex.EncodeToString(wantSHA256[:]) {
		t.Fatalf("sha256 = %q, want %q", got, hex.EncodeToString(wantSHA256[:]))
	}

	wantSHA512 := sha512.Sum512(body)
	if got := capturedString(t, result, "sha512"); got != hex.EncodeToString(wantSHA512[:]) {
		t.Fatalf("sha512 mismatch")
	}
}

// TestSaveWritesNFCUnstableBinaryBytes pins binary-safe downloads. The body
// 0x65 0xCC 0x81 ("e" + combining acute, U+0301) is NFC-recomposed to the
// 2-byte 0xC3 0xA9 ("é", U+00E9) when round-tripped through a go-cty string.
// save must write the exact 3 source bytes and report their true size/digest,
// never the NFC-normalized form.
func TestSaveWritesNFCUnstableBinaryBytes(t *testing.T) {
	t.Parallel()

	body := []byte{0x65, 0xCC, 0x81}

	step := newSaveStep()

	_, result := runSaveScenarioWithBody(t, step, body)

	stepResult := result.Scenarios[0].Steps[0]
	if stepResult.Status != report.StatusPass {
		t.Fatalf("step should pass: %#v", stepResult.Failure)
	}

	saved := capturedString(t, result, "path")

	onDisk, err := os.ReadFile(saved)
	if err != nil {
		t.Fatalf("read saved file: %v", err)
	}

	if !bytes.Equal(onDisk, body) {
		t.Fatalf("saved bytes are not byte-exact: got %x (%d bytes), want %x (%d bytes)", onDisk, len(onDisk), body, len(body))
	}

	wantSHA512 := sha512.Sum512(body)
	if got := capturedString(t, result, "sha512"); got != hex.EncodeToString(wantSHA512[:]) {
		t.Fatalf("response.download.sha512 = %q, want %q (must hash the true bytes)", got, hex.EncodeToString(wantSHA512[:]))
	}
}

func TestSaveCreatesParentDirectory(t *testing.T) {
	t.Parallel()

	step := newHTTPStep("download")
	step.Capture = map[string]model.Expression{"path": expr(`response.download.path`)}
	step.Expect = &model.Expect{Status: expr(`200`)}
	step.Save = &model.SaveBlock{Body: expr(`"a/b/c/deep.bin"`)}

	_, result, _ := runSaveScenario(t, step)

	if result.Scenarios[0].Steps[0].Status != report.StatusPass {
		t.Fatalf("step should pass: %#v", result.Scenarios[0].Steps[0].Failure)
	}

	saved := capturedString(t, result, "path")
	if _, err := os.Stat(saved); err != nil {
		t.Fatalf("expected nested file to exist: %v", err)
	}
}

func TestSaveRejectsTraversal(t *testing.T) {
	t.Parallel()

	step := newHTTPStep("download")
	step.Capture = map[string]model.Expression{}
	step.Expect = &model.Expect{Status: expr(`200`)}
	step.Save = &model.SaveBlock{Body: expr(`"../../outside.bin"`)}

	_, result, _ := runSaveScenario(t, step)

	failure := result.Scenarios[0].Steps[0].Failure
	if failure == nil {
		t.Fatal("expected save traversal to fail the step")
	}

	if failure.Kind != kindSave {
		t.Fatalf("expected kindSave failure, got %q", failure.Kind)
	}
}

// capturedString reads response.download.<key> from the only step's report,
// where applySaveAndDownload surfaced the download metadata.
func capturedString(t *testing.T, result *report.SuiteResult, key string) string {
	t.Helper()

	resp := result.Scenarios[0].Steps[0].Response
	download, ok := resp["download"].(map[string]any)
	if !ok {
		t.Fatalf("response.download missing in report: %#v", resp)
	}

	value, ok := download[key]
	if !ok {
		t.Fatalf("download.%s missing: %#v", key, download)
	}

	switch v := value.(type) {
	case string:
		return v
	default:
		t.Fatalf("download.%s is not a string: %T", key, value)

		return ""
	}
}
