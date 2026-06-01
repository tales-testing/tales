package parser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclparse"
)

// parseBody parses inline HCL native syntax for deprecation-walk tests.
func parseBody(t *testing.T, content string) hcl.Body {
	t.Helper()

	file, diags := hclparse.NewParser().ParseHCL([]byte(content), "test.tales")
	if diags.HasErrors() {
		t.Fatalf("parse failed: %s", diags.Error())
	}

	return file.Body
}

func TestLoadPathDeprecatedAliasesWarnNotError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	content := `version = 1

scenario "demo" {
  case "http" "b" {
    request {
      method = "GET"
      url = "http://example.test"
    }
    response {
      status = 200
    }
  }
}
`
	path := filepath.Join(dir, "demo.tales")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	suite, diags := LoadPath(dir)
	if diags.HasErrors() {
		t.Fatalf("deprecations must be warnings, not errors: %s", diags.Error())
	}

	if suite == nil {
		t.Fatal("suite should still load despite deprecated aliases")
	}

	warnings := Warnings(diags)
	if len(warnings) != 2 {
		t.Fatalf("want 2 deprecation warnings (case + response), got %d", len(warnings))
	}

	for _, w := range warnings {
		if w.Severity != hcl.DiagWarning {
			t.Errorf("diagnostic %q must have warning severity", w.Summary)
		}

		if w.Subject == nil || w.Subject.Filename != path {
			t.Errorf("warning %q must carry the source file as subject", w.Summary)
		}
	}

	// case is on line 4, response on line 9 — sorted by position.
	if got := warnings[0].Subject.Start.Line; got != 4 {
		t.Errorf("first warning (case) should point at line 4, got %d", got)
	}

	if got := warnings[1].Subject.Start.Line; got != 9 {
		t.Errorf("second warning (response) should point at line 9, got %d", got)
	}

	if !strings.Contains(warnings[0].Detail, `"step"`) {
		t.Errorf("case warning should suggest renaming to step: %q", warnings[0].Detail)
	}

	if !strings.Contains(warnings[1].Detail, `"expect"`) {
		t.Errorf("response warning should suggest renaming to expect: %q", warnings[1].Detail)
	}
}

func TestCollectDeprecationsCleanFileIsQuiet(t *testing.T) {
	t.Parallel()

	body := parseBody(t, `scenario "ok" {
  step "http" "a" {
    expect { status = 200 }
  }
}
`)

	if diags := collectDeprecations(body); len(diags) != 0 {
		t.Fatalf("a file using only canonical blocks must emit no warnings, got %d", len(diags))
	}
}

func TestCollectDeprecationsWithScopedAttribute(t *testing.T) {
	t.Parallel()

	body := parseBody(t, `scenario "demo" {
  step "http" "a" {
    legacy = true
  }
  legacy = false
}
`)

	rules := []deprecation{
		{
			kind:    deprecatedAttribute,
			name:    "legacy",
			within:  []string{"scenario", "step"},
			summary: "Deprecated attribute legacy",
			detail:  "use modern instead",
		},
	}

	diags := collectDeprecationsWith(body, rules)
	if len(diags) != 1 {
		t.Fatalf("scoped rule must match only the nested attribute, got %d", len(diags))
	}

	if diags[0].Subject.Start.Line != 3 {
		t.Errorf("matched attribute should be the one inside the step (line 3), got %d", diags[0].Subject.Start.Line)
	}
}

func TestMatchesWithin(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		within  []string
		parents []string
		want    bool
	}{
		{"empty matches anywhere", nil, []string{"scenario", "step"}, true},
		{"exact suffix", []string{"step"}, []string{"scenario", "step"}, true},
		{"multi suffix", []string{"scenario", "step"}, []string{"scenario", "step"}, true},
		{"suffix mismatch", []string{"keyword"}, []string{"scenario", "step"}, false},
		{"within longer than parents", []string{"a", "b", "c"}, []string{"b", "c"}, false},
		{"prefix is not a suffix", []string{"scenario"}, []string{"scenario", "step"}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := matchesWithin(tc.within, tc.parents); got != tc.want {
				t.Errorf("matchesWithin(%v, %v) = %v, want %v", tc.within, tc.parents, got, tc.want)
			}
		})
	}
}

func TestFormatDeprecation(t *testing.T) {
	t.Parallel()

	withSubject := &hcl.Diagnostic{
		Severity: hcl.DiagWarning,
		Detail:   "rename it",
		Subject:  &hcl.Range{Filename: "a.tales", Start: hcl.Pos{Line: 7}},
	}
	if got := FormatDeprecation(withSubject); got != "a.tales:7: rename it" {
		t.Errorf("unexpected format with subject: %q", got)
	}

	noSubject := &hcl.Diagnostic{Severity: hcl.DiagWarning, Detail: "rename it"}
	if got := FormatDeprecation(noSubject); got != "rename it" {
		t.Errorf("unexpected format without subject: %q", got)
	}
}

func TestWarningsFiltersBySeverity(t *testing.T) {
	t.Parallel()

	diags := hcl.Diagnostics{
		{Severity: hcl.DiagError, Summary: "boom"},
		{Severity: hcl.DiagWarning, Summary: "deprecated"},
		{Severity: hcl.DiagWarning, Summary: "deprecated again"},
	}

	if got := Warnings(diags); len(got) != 2 {
		t.Fatalf("want 2 warnings, got %d", len(got))
	}
}
