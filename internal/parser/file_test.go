package parser

import (
	"strings"
	"testing"
)

func TestLoadPathFileStepBasic(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "inspect" {
  step "file" "check" {
    path = "downloads/cert.pdf"
    expect {
      exists     = true
      size_bytes = gt(0)
      sha256     = "abc"
      text       = contains("PDF")
      json       = { valid = true }
    }
    capture {
      digest = file.sha256
    }
  }
}
`

	suite, diags := LoadPath(writeTales(t, content))
	if diags.HasErrors() {
		t.Fatalf("unexpected diagnostics: %s", diags.Error())
	}

	step := suite.Scenarios[0].Steps[0]
	if step.FileOp == nil {
		t.Fatal("expected step.FileOp to be populated")
	}

	if step.FileOp.Path.Empty() {
		t.Fatal("expected file path expression")
	}

	expect := step.FileOp.Expect
	if expect == nil {
		t.Fatal("expected file expect to be decoded")
	}

	if expect.Exists.Empty() || expect.SizeBytes.Empty() || expect.Text.Empty() || expect.JSON.Empty() {
		t.Fatal("expected exists/size_bytes/text/json to be decoded")
	}

	if _, ok := expect.Hashes["sha256"]; !ok {
		t.Fatal("expected sha256 hash assertion to be decoded")
	}
}

func TestLoadPathFileStepRequiresPath(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "inspect" {
  step "file" "check" {
    expect { exists = true }
  }
}
`

	_, diags := LoadPath(writeTales(t, content))
	if !diags.HasErrors() {
		t.Fatal("expected missing path to be rejected")
	}

	if !strings.Contains(diags.Error(), "file step must declare path") {
		t.Fatalf("unexpected message: %s", diags.Error())
	}
}

func TestLoadPathFileStepRejectsHTTPExpectAttrs(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "inspect" {
  step "file" "check" {
    path = "x.bin"
    expect {
      status = 200
    }
  }
}
`

	_, diags := LoadPath(writeTales(t, content))
	if !diags.HasErrors() {
		t.Fatal("expected HTTP-only status attribute to be rejected on a file step")
	}

	if !strings.Contains(diags.Error(), "file expect does not support") {
		t.Fatalf("unexpected message: %s", diags.Error())
	}
}

func TestLoadPathFileStepRejectsUnknownExpectAttr(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "inspect" {
  step "file" "check" {
    path = "x.bin"
    expect {
      whatever = true
    }
  }
}
`

	_, diags := LoadPath(writeTales(t, content))
	if !diags.HasErrors() {
		t.Fatal("expected unknown expect attribute to be rejected")
	}

	if !strings.Contains(diags.Error(), "Unknown file expect attribute") {
		t.Fatalf("unexpected message: %s", diags.Error())
	}
}

func TestLoadPathPathOnNonFileStepRejected(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "inspect" {
  step "http" "fetch" {
    path = "downloads/cert.pdf"
    request {
      method = "GET"
      url    = "http://localhost/x"
    }
  }
}
`

	_, diags := LoadPath(writeTales(t, content))
	if !diags.HasErrors() {
		t.Fatal("expected step-level path on an http step to be rejected")
	}

	if !strings.Contains(diags.Error(), "file-only") {
		t.Fatalf("unexpected message: %s", diags.Error())
	}
}
