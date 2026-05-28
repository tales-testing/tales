package parser

import (
	"strings"
	"testing"
)

func TestLoadPathLoadStepBasic(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "load smoke" {
  step "load" "healthz" {
    http {
      method = "GET"
      url    = "http://localhost:1337/healthz"
    }
    run {
      requests    = 50
      concurrency = 5
    }
    expect {
      status_2xx_ratio = gte(1.0)
      p95              = lt("1s")
    }
  }
}
`

	suite, diags := LoadPath(writeTales(t, content))
	if diags.HasErrors() {
		t.Fatalf("unexpected diagnostics: %s", diags.Error())
	}

	step := suite.Scenarios[0].Steps[0]
	if step.Load == nil {
		t.Fatal("expected step.Load to be populated")
	}

	if step.Load.Request == nil {
		t.Fatal("expected http request decoded")
	}

	if step.Load.Run == nil {
		t.Fatal("expected run block decoded")
	}

	if step.Load.Run.Requests.Empty() {
		t.Fatalf("expected requests expression")
	}

	if !step.Load.Run.Duration.Empty() {
		t.Fatalf("duration must remain empty when only requests is set")
	}
}

func TestLoadPathLoadRejectsBothDurationAndRequests(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "load smoke" {
  step "load" "healthz" {
    http {
      method = "GET"
      url    = "http://localhost:1337/healthz"
    }
    run {
      duration = "5s"
      requests = 10
    }
    expect {}
  }
}
`

	_, diags := LoadPath(writeTales(t, content))
	if !diags.HasErrors() {
		t.Fatalf("expected duration+requests rejection")
	}

	if !strings.Contains(diags.Error(), "Conflicting run mode") {
		t.Fatalf("unexpected error: %s", diags.Error())
	}
}

func TestLoadPathLoadRequiresHTTPBlock(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "load smoke" {
  step "load" "noop" {
    run {
      requests    = 1
      concurrency = 1
    }
    expect {}
  }
}
`

	_, diags := LoadPath(writeTales(t, content))
	if !diags.HasErrors() {
		t.Fatalf("expected missing http block error")
	}

	if !strings.Contains(diags.Error(), "Missing http block") {
		t.Fatalf("unexpected error: %s", diags.Error())
	}
}

func TestLoadPathLoadRejectsRunOnNonLoadStep(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "demo" {
  step "http" "smoke" {
    request {
      method = "GET"
      url    = "http://example.com"
    }
    run {
      requests    = 1
      concurrency = 1
    }
    expect {}
  }
}
`

	_, diags := LoadPath(writeTales(t, content))
	if !diags.HasErrors() {
		t.Fatalf("expected rejection on non-load step")
	}

	if !strings.Contains(diags.Error(), "Load fields on non-load step") {
		t.Fatalf("unexpected error: %s", diags.Error())
	}
}
