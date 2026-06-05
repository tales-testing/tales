package parser

import (
	"strings"
	"testing"
)

func TestLoadPathSaveBlockOnHTTPStep(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "download" {
  step "http" "fetch" {
    request {
      method = "GET"
      url    = "http://localhost:1337/files/x.pdf"
    }
    expect { status = 200 }
    save { body = "downloads/x.pdf" }
  }
}
`

	suite, diags := LoadPath(writeTales(t, content))
	if diags.HasErrors() {
		t.Fatalf("unexpected diagnostics: %s", diags.Error())
	}

	step := suite.Scenarios[0].Steps[0]
	if step.Save == nil {
		t.Fatal("expected step.Save to be populated")
	}

	if step.Save.Body.Empty() {
		t.Fatal("expected save.body expression to be set")
	}
}

func TestLoadPathSaveBlockRejectedOnNonHTTPStep(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "bad" {
  step "sql" "q" {
    connection = "main"
    query { sql = "SELECT 1" }
    save { body = "downloads/x.pdf" }
  }
}
`

	_, diags := LoadPath(writeTales(t, content))
	if !diags.HasErrors() {
		t.Fatal("expected save on a non-http step to be rejected")
	}

	if !strings.Contains(diags.Error(), "only valid on http steps") {
		t.Fatalf("expected clear rejection message, got: %s", diags.Error())
	}
}

func TestLoadPathSaveBlockRequiresBody(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "download" {
  step "http" "fetch" {
    request {
      method = "GET"
      url    = "http://localhost:1337/x"
    }
    save {}
  }
}
`

	_, diags := LoadPath(writeTales(t, content))
	if !diags.HasErrors() {
		t.Fatal("expected empty save block to be rejected")
	}

	if !strings.Contains(diags.Error(), "save block must declare body") {
		t.Fatalf("expected missing-body message, got: %s", diags.Error())
	}
}
