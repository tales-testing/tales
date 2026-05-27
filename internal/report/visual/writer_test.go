package visual

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/tales-testing/tales/internal/report"
)

func TestWriteProducesFileWithEmbeddedAssets(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "visual.html")

	suite := sampleSuite()

	if err := Write(path, suite); err != nil {
		t.Fatalf("Write: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	body := string(raw)

	for _, want := range []string{
		"<!doctype html>",
		"id=\"tales-report-data\"",
		"<style>",
		"<script>",
		"--accent-pass",
		"Tales Visual Report",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in rendered output", want)
		}
	}
}

func TestWriteEmbedsParseableJSONPayload(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "visual.html")
	if err := Write(path, sampleSuite()); err != nil {
		t.Fatalf("Write: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	re := regexp.MustCompile(`(?s)<script type="application/json" id="tales-report-data">(.*?)</script>`)
	m := re.FindStringSubmatch(string(raw))
	if len(m) != 2 {
		t.Fatalf("data island not found")
	}

	var decoded Report
	if err := json.Unmarshal([]byte(m[1]), &decoded); err != nil {
		t.Fatalf("data island is not valid JSON: %v", err)
	}

	if len(decoded.Scenarios) != 1 || decoded.Scenarios[0].Name != "register" {
		t.Fatalf("decoded report missing expected scenario: %+v", decoded)
	}
}

func TestWriteDefusesScriptTagInScenarioName(t *testing.T) {
	t.Parallel()

	suite := &report.SuiteResult{
		Scenarios: []*report.ScenarioResult{
			{
				Name:     "evil </script><img src=x>",
				Status:   report.StatusPass,
				Duration: time.Millisecond,
				Steps: []*report.StepResult{{
					Name: "step", Status: report.StatusPass,
					Actions: []*report.ActionResult{{Index: 0, Kind: "tap", Status: report.StatusPass}},
				}},
			},
		},
	}

	path := filepath.Join(t.TempDir(), "visual.html")
	if err := Write(path, suite); err != nil {
		t.Fatalf("Write: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	body := string(raw)

	// The legitimate inline script tags appear twice (data island + js
	// block), each with one </script> close. The data-island payload must
	// never contain a third raw "</script>" — encoding-time escaping
	// (json HTML-escape + the ReplaceAll defuse layer) must keep the
	// scenario name's "</script>" out as a literal sequence.
	re := regexp.MustCompile(`(?s)<script type="application/json" id="tales-report-data">(.*?)</script>`)
	m := re.FindStringSubmatch(body)
	if len(m) != 2 {
		t.Fatalf("data island not found")
	}

	if strings.Contains(m[1], "</script>") {
		t.Errorf("data island contains a raw </script> sequence; HTML data island can be broken out of: %q", m[1])
	}
}

func TestWriteMasksSecureValues(t *testing.T) {
	t.Parallel()

	suite := &report.SuiteResult{
		Scenarios: []*report.ScenarioResult{{
			Name: "secrets", Status: report.StatusPass,
			Steps: []*report.StepResult{{
				Name: "submit", Status: report.StatusPass,
				Actions: []*report.ActionResult{{
					Index: 0, Kind: "input_text",
					Label:      "Input text register.password ***",
					SelectorID: "register.password",
					Secure:     true,
					Value:      "***",
					Status:     report.StatusPass,
				}},
			}},
		}},
	}

	path := filepath.Join(t.TempDir(), "visual.html")
	if err := Write(path, suite); err != nil {
		t.Fatalf("Write: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	body := string(raw)
	if strings.Contains(body, "hunter2") || strings.Contains(body, "rawvalue") {
		t.Fatalf("output appears to contain a raw secret value")
	}

	if !strings.Contains(body, "***") {
		t.Errorf("expected the mask *** to appear in the rendered output")
	}
}

func TestWriteHandlesEmptySuite(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "visual.html")
	if err := Write(path, &report.SuiteResult{}); err != nil {
		t.Fatalf("Write empty: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	if !strings.Contains(string(raw), "No visual actions were captured") {
		t.Errorf("expected empty-state copy in the rendered output")
	}
}
