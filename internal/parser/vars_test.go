package parser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeVarsTale(t *testing.T, content string) string {
	t.Helper()

	dir := t.TempDir()

	path := filepath.Join(dir, "vars.tales")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	return dir
}

func TestLoadPathVarsBlockPreservesOrder(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "vars" {
  step "http" "send" {
    vars {
      ts   = "first"
      body = "second"
      sig  = "third"
    }
    request {
      method = "POST"
      url    = "http://example.test"
      headers = {
        X-Sig = vars.sig
      }
      body {
        raw = vars.body
      }
    }
  }
}
`

	suite, diags := LoadPath(writeVarsTale(t, content))
	if diags.HasErrors() {
		t.Fatalf("unexpected diagnostics: %s", diags.Error())
	}

	step := suite.Scenarios[0].Steps[0]
	if len(step.Vars) != 3 {
		t.Fatalf("want 3 vars, got %d", len(step.Vars))
	}

	wantNames := []string{"ts", "body", "sig"}
	for i, name := range wantNames {
		if step.Vars[i].Name != name {
			t.Fatalf("var %d: want %q, got %q", i, name, step.Vars[i].Name)
		}
	}
}

func TestLoadPathVarsCanReferenceEarlierVar(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "vars" {
  step "http" "send" {
    vars {
      a = "hello"
      b = "${vars.a} world"
    }
    request {
      method = "GET"
      url    = "http://example.test"
      headers = {
        X-Test = vars.b
      }
    }
  }
}
`

	if _, diags := LoadPath(writeVarsTale(t, content)); diags.HasErrors() {
		t.Fatalf("unexpected diagnostics: %s", diags.Error())
	}
}

func TestLoadPathVarsForwardReferenceIsRejected(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "vars" {
  step "http" "send" {
    vars {
      b = vars.a
      a = "hello"
    }
    request {
      method = "GET"
      url    = "http://example.test"
    }
  }
}
`

	_, diags := LoadPath(writeVarsTale(t, content))
	if !diags.HasErrors() {
		t.Fatalf("expected forward reference error")
	}

	if !strings.Contains(diags.Error(), "before it is defined") {
		t.Fatalf("expected 'before it is defined' diagnostic, got: %s", diags.Error())
	}
}

func TestLoadPathVarsSelfReferenceIsRejected(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "vars" {
  step "http" "send" {
    vars {
      a = "${vars.a}-loop"
    }
    request {
      method = "GET"
      url    = "http://example.test"
    }
  }
}
`

	_, diags := LoadPath(writeVarsTale(t, content))
	if !diags.HasErrors() {
		t.Fatalf("expected self-reference error")
	}

	if !strings.Contains(diags.Error(), "cannot reference itself") {
		t.Fatalf("expected 'cannot reference itself' diagnostic, got: %s", diags.Error())
	}
}

func TestLoadPathVarsUnknownReferenceInRequestIsRejected(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "vars" {
  step "http" "send" {
    vars {
      a = "hello"
    }
    request {
      method = "GET"
      url    = "http://example.test"
      headers = {
        X-Test = vars.does_not_exist
      }
    }
  }
}
`

	_, diags := LoadPath(writeVarsTale(t, content))
	if !diags.HasErrors() {
		t.Fatalf("expected unknown var error")
	}

	if !strings.Contains(diags.Error(), "unknown variable") {
		t.Fatalf("expected 'unknown variable' diagnostic, got: %s", diags.Error())
	}
}

func TestLoadPathVarsCrossStepReferenceIsRejected(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "vars" {
  step "http" "first" {
    vars {
      shared = "value"
    }
    request {
      method = "GET"
      url    = "http://example.test"
    }
  }
  step "http" "second" {
    request {
      method = "GET"
      url    = "http://example.test"
      headers = {
        X-Shared = vars.shared
      }
    }
  }
}
`

	_, diags := LoadPath(writeVarsTale(t, content))
	if !diags.HasErrors() {
		t.Fatalf("expected cross-step var error")
	}
}

func TestLoadPathVarsEmptyBlockIsAccepted(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "vars" {
  step "http" "send" {
    vars {
    }
    request {
      method = "GET"
      url    = "http://example.test"
    }
  }
}
`

	suite, diags := LoadPath(writeVarsTale(t, content))
	if diags.HasErrors() {
		t.Fatalf("unexpected diagnostics: %s", diags.Error())
	}

	if got := len(suite.Scenarios[0].Steps[0].Vars); got != 0 {
		t.Fatalf("want 0 vars, got %d", got)
	}
}

func TestLoadPathVarsRejectsDuplicateBlocks(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "vars" {
  step "http" "send" {
    vars {
      a = "first"
    }
    vars {
      b = "second"
    }
    request {
      method = "GET"
      url    = "http://example.test"
    }
  }
}
`

	_, diags := LoadPath(writeVarsTale(t, content))
	if !diags.HasErrors() {
		t.Fatalf("expected duplicate vars block error")
	}
}

func TestLoadPathVarsRejectsNestedBlock(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "vars" {
  step "http" "send" {
    vars {
      nested {
        x = 1
      }
    }
    request {
      method = "GET"
      url    = "http://example.test"
    }
  }
}
`

	_, diags := LoadPath(writeVarsTale(t, content))
	if !diags.HasErrors() {
		t.Fatalf("expected nested block error")
	}

	if !strings.Contains(diags.Error(), "vars supports attributes only") {
		t.Fatalf("expected 'attributes only' diagnostic, got: %s", diags.Error())
	}
}

func TestLoadPathVarsAvailableInExpectAndCapture(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "vars" {
  step "http" "send" {
    vars {
      marker = "abc"
    }
    request {
      method = "GET"
      url    = "http://example.test"
    }
    expect {
      headers = {
        X-Echo = vars.marker
      }
    }
    capture {
      echoed = vars.marker
    }
  }
}
`

	if _, diags := LoadPath(writeVarsTale(t, content)); diags.HasErrors() {
		t.Fatalf("unexpected diagnostics: %s", diags.Error())
	}
}
