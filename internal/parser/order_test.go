package parser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/tales-testing/tales/internal/model"
)

func loadString(t *testing.T, content string) (*model.Suite, hcl.Diagnostics) {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "order.tales")

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	return LoadPath(dir)
}

func names(steps []*model.Step) []string {
	out := make([]string, 0, len(steps))
	for _, step := range steps {
		out = append(out, step.Name)
	}

	return out
}

func assertOrder(t *testing.T, label string, got, want []string) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("%s: want %v got %v", label, want, got)
	}

	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s: want %v got %v", label, want, got)
		}
	}
}

// TestInterleavedStepAndCaseSourceOrder ensures that mixing step and case
// blocks preserves their textual order in model.Scenario.Steps, instead of
// the gohcl default of all step blocks followed by all case blocks.
func TestInterleavedStepAndCaseSourceOrder(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "interleaved" {
  step "test" "first" {}
  case "test" "second" {}
  step "test" "third" {}
  case "test" "fourth" {}
}
`
	suite, diags := loadString(t, content)
	if diags.HasErrors() {
		t.Fatalf("unexpected diagnostics: %s", diags.Error())
	}

	got := names(suite.Scenarios[0].Steps)
	want := []string{"first", "second", "third", "fourth"}
	assertOrder(t, "scenario steps", got, want)
}

// TestInterleavedTeardownSourceOrder ensures teardown step/case ordering.
func TestInterleavedTeardownSourceOrder(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "interleaved" {
  step "test" "main" {}

  teardown {
    step "test" "cleanup_a" {}
    case "test" "cleanup_b" {}
    step "test" "cleanup_c" {}
  }
}
`
	suite, diags := loadString(t, content)
	if diags.HasErrors() {
		t.Fatalf("unexpected diagnostics: %s", diags.Error())
	}

	got := names(suite.Scenarios[0].Teardown)
	want := []string{"cleanup_a", "cleanup_b", "cleanup_c"}
	assertOrder(t, "teardown steps", got, want)
}

// TestInterleavedKeywordSourceOrder ensures keyword sub-step ordering.
func TestInterleavedKeywordSourceOrder(t *testing.T) {
	t.Parallel()

	content := `version = 1

keyword "flow" {
  step "test" "k_first" {}
  case "test" "k_second" {}
  step "test" "k_third" {}
}
`
	suite, diags := loadString(t, content)
	if diags.HasErrors() {
		t.Fatalf("unexpected diagnostics: %s", diags.Error())
	}

	got := names(suite.Keywords["flow"].Steps)
	want := []string{"k_first", "k_second", "k_third"}
	assertOrder(t, "keyword steps", got, want)
}

// TestStepLineFromBlockDefRange ensures Step.Line points at the real block.
func TestStepLineFromBlockDefRange(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "lines" {
  step "test" "first" {}
  step "test" "second" {}
}
`
	suite, diags := loadString(t, content)
	if diags.HasErrors() {
		t.Fatalf("unexpected diagnostics: %s", diags.Error())
	}

	steps := suite.Scenarios[0].Steps
	if steps[0].Line != 4 {
		t.Fatalf("first step: want line 4 got %d", steps[0].Line)
	}

	if steps[1].Line != 5 {
		t.Fatalf("second step: want line 5 got %d", steps[1].Line)
	}
}

// TestLoadRejectsForwardResultReference ensures a step referencing a step
// defined later in the file is rejected at load time.
func TestLoadRejectsForwardResultReference(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "forward" {
  step "http" "second" {
    request {
      method = "GET"
      url    = "http://x/${result.first.id}"
    }
  }
  step "http" "first" {}
}
`
	_, diags := loadString(t, content)
	if !diags.HasErrors() {
		t.Fatal("expected forward reference to be rejected at load time")
	}

	if !strings.Contains(diags.Error(), `references result.first, but "first" is defined later`) {
		t.Fatalf("unexpected diagnostics: %s", diags.Error())
	}
}

// TestLoadRejectsForwardDependsOn ensures a forward depends_on is rejected.
func TestLoadRejectsForwardDependsOn(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "forward" {
  step "http" "login" {
    depends_on = ["create_user"]
  }
  step "http" "create_user" {}
}
`
	_, diags := loadString(t, content)
	if !diags.HasErrors() {
		t.Fatal("expected forward depends_on to be rejected at load time")
	}

	if !strings.Contains(diags.Error(), `depends on "create_user", but "create_user" is defined later`) {
		t.Fatalf("unexpected diagnostics: %s", diags.Error())
	}
}

// TestLoadAcceptsBackwardReference confirms a top-down scenario still loads.
func TestLoadAcceptsBackwardReference(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "ok" {
  step "http" "first" {}
  step "http" "second" {
    request {
      method = "GET"
      url    = "http://x/${result.first.id}"
    }
  }
}
`
	_, diags := loadString(t, content)
	if diags.HasErrors() {
		t.Fatalf("backward reference must load cleanly: %s", diags.Error())
	}
}

// TestLoadRejectsDuplicateKeywordStepNames ensures keyword sub-steps with the
// same name are rejected at load time (they break source-order reordering and
// result lookup).
func TestLoadRejectsDuplicateKeywordStepNames(t *testing.T) {
	t.Parallel()

	content := `version = 1

keyword "flow" {
  step "test" "dup" {}
  step "test" "dup" {}
}
`
	_, diags := loadString(t, content)
	if !diags.HasErrors() {
		t.Fatal("expected duplicate keyword step names to be rejected")
	}

	if !strings.Contains(diags.Error(), `Keyword "flow" has duplicate step "dup"`) {
		t.Fatalf("unexpected diagnostics: %s", diags.Error())
	}
}
