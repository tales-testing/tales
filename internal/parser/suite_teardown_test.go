package parser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSuiteFile(t *testing.T, dir, name, content string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}

	return path
}

const suiteTeardownFile = `version = 1

teardown {
  step "http" "purge" {
    request {
      method = "DELETE"
      url    = "http://localhost:1337/users"
    }
    expect {
      status = 204
    }
  }

  case "http" "drop_sandbox" {
    request {
      method = "DELETE"
      url    = "http://localhost:1337/sandbox"
    }
    expect {
      status = 204
    }
  }
}

scenario "demo" {
  step "http" "a" {
    request {
      method = "GET"
      url    = "http://localhost:1337/healthz"
    }
    expect {
      status = 200
    }
  }
}
`

func TestLoadPathDecodesSuiteTeardown(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := writeSuiteFile(t, dir, "suite.tales", suiteTeardownFile)

	suite, diags := LoadPath(path)
	if diags.HasErrors() {
		t.Fatalf("unexpected diagnostics: %s", diags.Error())
	}

	if len(suite.Teardown) != 2 {
		t.Fatalf("suite teardown steps = %d, want 2", len(suite.Teardown))
	}

	// The `case` alias must keep its textual position relative to `step`.
	if suite.Teardown[0].Name != "purge" || suite.Teardown[1].Name != "drop_sandbox" {
		t.Fatalf("suite teardown order = [%s %s], want [purge drop_sandbox]", suite.Teardown[0].Name, suite.Teardown[1].Name)
	}

	if suite.TeardownFile != path {
		t.Fatalf("teardown file = %q, want %q", suite.TeardownFile, path)
	}

	// A suite teardown must not leak into the scenario's own teardown.
	if len(suite.Scenarios[0].Teardown) != 0 {
		t.Fatalf("scenario teardown = %d, want 0", len(suite.Scenarios[0].Teardown))
	}
}

// Concatenating suite teardowns across files would make the cleanup order
// depend on the alphabetical order of filenames, so a second declaration is
// an error rather than a silent merge.
func TestLoadPathRejectsTwoSuiteTeardowns(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeSuiteFile(t, dir, "a_suite.tales", suiteTeardownFile)
	writeSuiteFile(t, dir, "b_suite.tales", `version = 1

teardown {
  step "http" "other" {
    request {
      method = "DELETE"
      url    = "http://localhost:1337/other"
    }
    expect {
      status = 204
    }
  }
}
`)

	_, diags := LoadPath(dir)
	if !diags.HasErrors() {
		t.Fatal("expected a duplicate suite teardown diagnostic")
	}

	detail := ""

	for _, diag := range diags {
		if diag.Summary == "Duplicate suite teardown" {
			detail = diag.Detail
		}
	}

	if detail == "" {
		t.Fatalf("diagnostics = %s, want a duplicate suite teardown error", diags.Error())
	}

	// Files are loaded in sorted order, so the message must blame the first.
	if !strings.Contains(detail, "a_suite.tales") {
		t.Fatalf("detail = %q, want it to name the owning file", detail)
	}
}

func TestLoadPathRejectsDuplicateSuiteTeardownStepNames(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := writeSuiteFile(t, dir, "suite.tales", `version = 1

teardown {
  step "http" "purge" {
    request {
      method = "DELETE"
      url    = "http://localhost:1337/a"
    }
    expect {
      status = 204
    }
  }

  step "http" "purge" {
    request {
      method = "DELETE"
      url    = "http://localhost:1337/b"
    }
    expect {
      status = 204
    }
  }
}
`)

	_, diags := LoadPath(path)
	if !diags.HasErrors() {
		t.Fatal("expected a duplicate step diagnostic")
	}

	found := false

	for _, diag := range diags {
		if strings.Contains(diag.Detail, "Suite teardown has duplicate step") {
			found = true
		}
	}

	if !found {
		t.Fatalf("diagnostics = %s, want a duplicate step error", diags.Error())
	}
}
