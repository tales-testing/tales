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

func TestLoadPathRetryBlock(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	content := `version = 1

scenario "retry" {
  step "http" "eventual" {
    retry {
      attempts = 3
      interval = "10ms"
    }
    request {
      method = "GET"
      url = "http://example.test"
    }
    expect {
      status = 200
      body = contains("ok")
    }
  }
}
`
	path := filepath.Join(dir, "retry.tales")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	suite, diags := LoadPath(dir)
	if diags.HasErrors() {
		t.Fatalf("unexpected diagnostics: %s", diags.Error())
	}

	step := suite.Scenarios[0].Steps[0]
	if step.Retry == nil {
		t.Fatalf("retry block was not decoded")
	}

	if step.Retry.Attempts != 3 {
		t.Fatalf("attempts=%d", step.Retry.Attempts)
	}

	if step.Expect.Body.Empty() {
		t.Fatalf("expect.body was not decoded")
	}
}

func TestLoadPathRequestBasicAuth(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	content := `version = 1

config {
  basic_auth = {
    username = "admin"
    password = "secret"
  }
}

scenario "basic auth" {
  step "http" "protected" {
    request {
      method = "GET"
      url = "http://example.test"
      auth {
        basic {
          username = config.basic_auth.username
          password = config.basic_auth.password
        }
      }
    }
  }
}
`
	path := filepath.Join(dir, "basic.tales")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	suite, diags := LoadPath(dir)
	if diags.HasErrors() {
		t.Fatalf("unexpected diagnostics: %s", diags.Error())
	}

	auth := suite.Scenarios[0].Steps[0].Request.Auth
	if auth == nil || auth.Basic == nil {
		t.Fatalf("basic auth was not decoded")
	}

	if auth.Basic.Username.Empty() {
		t.Fatalf("basic auth username expression was not decoded")
	}

	if auth.Basic.Password.Empty() {
		t.Fatalf("basic auth password expression was not decoded")
	}
}

func TestLoadPathRequestForm(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	content := `version = 1

scenario "form" {
  step "http" "submit" {
    request {
      method = "POST"
      url = "http://example.test"
      body {
        form = {
          value = "a&b=c"
        }
      }
    }
  }
}
`
	path := filepath.Join(dir, "form.tales")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	suite, diags := LoadPath(dir)
	if diags.HasErrors() {
		t.Fatalf("unexpected diagnostics: %s", diags.Error())
	}

	body := suite.Scenarios[0].Steps[0].Request.Body
	if body == nil || body.Form.Empty() {
		t.Fatalf("request.body.form was not decoded")
	}
}

func TestLoadPathRequestBodyRejectsMultipleModes(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	content := `version = 1

scenario "form conflict" {
  step "http" "submit" {
    request {
      method = "POST"
      url = "http://example.test"
      body {
        form = {
          value = "a&b=c"
        }
        raw = "value=a%26b%3Dc"
      }
    }
  }
}
`
	path := filepath.Join(dir, "form.tales")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	_, diags := LoadPath(dir)
	if !diags.HasErrors() {
		t.Fatalf("expected diagnostics")
	}
}

func TestLoadPathBasicAuthMissingUsername(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	content := `version = 1

scenario "basic auth" {
  step "http" "protected" {
    request {
      method = "GET"
      url = "http://example.test"
      auth {
        basic {
          password = "secret"
        }
      }
    }
  }
}
`
	path := filepath.Join(dir, "basic.tales")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	_, diags := LoadPath(dir)
	if !diags.HasErrors() {
		t.Fatalf("expected diagnostics")
	}
}

func TestLoadPathBasicAuthMissingPassword(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	content := `version = 1

scenario "basic auth" {
  step "http" "protected" {
    request {
      method = "GET"
      url = "http://example.test"
      auth {
        basic {
          username = "admin"
        }
      }
    }
  }
}
`
	path := filepath.Join(dir, "basic.tales")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	_, diags := LoadPath(dir)
	if !diags.HasErrors() {
		t.Fatalf("expected diagnostics")
	}
}

func TestLoadPathDuplicateAuthBlocks(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	content := `version = 1

scenario "basic auth" {
  step "http" "protected" {
    request {
      method = "GET"
      url = "http://example.test"
      auth {
        basic {
          username = "admin"
          password = "secret"
        }
      }
      auth {
        basic {
          username = "admin"
          password = "secret"
        }
      }
    }
  }
}
`
	path := filepath.Join(dir, "basic.tales")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	_, diags := LoadPath(dir)
	if !diags.HasErrors() {
		t.Fatalf("expected diagnostics")
	}
}

func TestLoadPathDuplicateBasicAuthBlocks(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	content := `version = 1

scenario "basic auth" {
  step "http" "protected" {
    request {
      method = "GET"
      url = "http://example.test"
      auth {
        basic {
          username = "admin"
          password = "secret"
        }
        basic {
          username = "admin"
          password = "secret"
        }
      }
    }
  }
}
`
	path := filepath.Join(dir, "basic.tales")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	_, diags := LoadPath(dir)
	if !diags.HasErrors() {
		t.Fatalf("expected diagnostics")
	}
}

func TestLoadPathUnknownAuthScheme(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	content := `version = 1

scenario "basic auth" {
  step "http" "protected" {
    request {
      method = "GET"
      url = "http://example.test"
      auth {
        bearer {
          token = "abc"
        }
      }
    }
  }
}
`
	path := filepath.Join(dir, "basic.tales")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	_, diags := LoadPath(dir)
	if !diags.HasErrors() {
		t.Fatalf("expected diagnostics")
	}
}

func TestLoadPathInvalidRetryAttempts(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	content := `version = 1

scenario "retry" {
  step "http" "bad" {
    retry {
      attempts = 0
    }
    request {
      method = "GET"
      url = "http://example.test"
    }
  }
}
`
	path := filepath.Join(dir, "retry.tales")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	_, diags := LoadPath(dir)
	if !diags.HasErrors() {
		t.Fatalf("expected diagnostics")
	}
}

func TestLoadPathInvalidRetryInterval(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	content := `version = 1

scenario "retry" {
  step "http" "bad" {
    retry {
      interval = "not-a-duration"
    }
    request {
      method = "GET"
      url = "http://example.test"
    }
  }
}
`
	path := filepath.Join(dir, "retry.tales")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	_, diags := LoadPath(dir)
	if !diags.HasErrors() {
		t.Fatalf("expected diagnostics")
	}
}
