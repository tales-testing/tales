package parser

import (
	"strings"
	"testing"
)

func TestLoadPathExecStepBasic(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "verify" {
  step "exec" "run" {
    command = "./bin/verify"
    args    = ["--in", "x.pdf"]
    env     = { STRICT = "1" }
    timeout = "10s"
    sandbox {
      mode    = "process"
      workdir = "scenario"
      env     = "minimal"
      network = false
    }
    expect {
      exit_code   = 0
      stdout_json = { valid = true }
    }
    capture {
      root = stdout.json.merkle.root
    }
  }
}
`

	suite, diags := LoadPath(writeTales(t, content))
	if diags.HasErrors() {
		t.Fatalf("unexpected diagnostics: %s", diags.Error())
	}

	step := suite.Scenarios[0].Steps[0]
	if step.Exec == nil {
		t.Fatal("expected step.Exec to be populated")
	}

	if step.Exec.Command.Empty() || step.Exec.Args.Empty() {
		t.Fatal("expected command and args to be decoded")
	}

	if step.Exec.Sandbox == nil {
		t.Fatal("expected sandbox block to be decoded")
	}

	if step.Exec.Expect == nil || step.Exec.Expect.ExitCode.Empty() || step.Exec.Expect.StdoutJSON.Empty() {
		t.Fatal("expected exit_code and stdout_json assertions")
	}
}

// TestLoadPathExecParsesWithoutAllowExec confirms validation/parsing of an
// exec step never depends on the --allow-exec runtime flag.
func TestLoadPathExecParsesWithoutAllowExec(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "verify" {
  step "exec" "run" {
    command = "verify-tool"
  }
}
`

	suite, diags := LoadPath(writeTales(t, content))
	if diags.HasErrors() {
		t.Fatalf("exec steps must parse without --allow-exec: %s", diags.Error())
	}

	if suite.Scenarios[0].Steps[0].Exec == nil {
		t.Fatal("expected exec step to decode")
	}
}

func TestLoadPathExecRequiresCommand(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "verify" {
  step "exec" "run" {
    args = ["x"]
  }
}
`

	_, diags := LoadPath(writeTales(t, content))
	if !diags.HasErrors() {
		t.Fatal("expected missing command to be rejected")
	}

	if !strings.Contains(diags.Error(), "exec step must declare command") {
		t.Fatalf("unexpected message: %s", diags.Error())
	}
}

func TestLoadPathExecRejectsJSONExpectAttr(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "verify" {
  step "exec" "run" {
    command = "x"
    expect {
      json = { valid = true }
    }
  }
}
`

	_, diags := LoadPath(writeTales(t, content))
	if !diags.HasErrors() {
		t.Fatal("expected json expect attribute to be rejected on exec")
	}

	if !strings.Contains(diags.Error(), "use stdout_json") {
		t.Fatalf("unexpected message: %s", diags.Error())
	}
}

func TestLoadPathExecRejectsUnknownExpectAttr(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "verify" {
  step "exec" "run" {
    command = "x"
    expect {
      bogus = 1
    }
  }
}
`

	_, diags := LoadPath(writeTales(t, content))
	if !diags.HasErrors() {
		t.Fatal("expected unknown exec expect attribute to be rejected")
	}

	if !strings.Contains(diags.Error(), "Unknown exec expect attribute") {
		t.Fatalf("unexpected message: %s", diags.Error())
	}
}

func TestLoadPathSandboxOnNonExecStepRejected(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "verify" {
  step "http" "fetch" {
    request {
      method = "GET"
      url    = "http://localhost/x"
    }
    sandbox { mode = "process" }
  }
}
`

	_, diags := LoadPath(writeTales(t, content))
	if !diags.HasErrors() {
		t.Fatal("expected sandbox on a non-exec step to be rejected")
	}

	if !strings.Contains(diags.Error(), "exec-only") {
		t.Fatalf("unexpected message: %s", diags.Error())
	}
}
