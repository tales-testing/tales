package parser

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadPathScenarioStepAndAliases(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	content := `version = 1

config {
  base_url = "http://localhost:1337"
}

generator "email" "user_email" {
  prefix = "test-"
}

scenario "demo" {
  step "http" "a" {
    request {
      method = "GET"
      url = config.base_url
    }
    expect {
      status = 200
    }
  }

  case "http" "b" {
    request {
      method = "GET"
      url = config.base_url
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
		t.Fatalf("unexpected diagnostics: %s", diags.Error())
	}
	if len(suite.Scenarios) != 1 {
		t.Fatalf("want 1 scenario got %d", len(suite.Scenarios))
	}
	if len(suite.Scenarios[0].Steps) != 2 {
		t.Fatalf("want 2 steps got %d", len(suite.Scenarios[0].Steps))
	}
	if suite.Scenarios[0].Steps[1].Expect == nil {
		t.Fatalf("response alias was not mapped to expect")
	}
}
