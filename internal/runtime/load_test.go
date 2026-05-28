package runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/tales-testing/tales/internal/provider"
)

const fractionalConcurrencyTales = `version = 1

config {
  base_url = "http://localhost:1337"
}

scenario "fractional concurrency" {
  step "load" "smoke" {
    http {
      method = "GET"
      url    = "${config.base_url}/healthz"
    }
    run {
      requests    = 1
      concurrency = 2.5
    }
    expect {}
  }
}
`

const fractionalRequestsTales = `version = 1

config {
  base_url = "http://localhost:1337"
}

scenario "fractional requests" {
  step "load" "smoke" {
    http {
      method = "GET"
      url    = "${config.base_url}/healthz"
    }
    run {
      requests    = 1.9
      concurrency = 1
    }
    expect {}
  }
}
`

func TestRunnerRejectsFractionalLoadConcurrency(t *testing.T) {
	t.Parallel()

	suite := loadTales(t, fractionalConcurrencyTales)

	runner := NewRunner(provider.NewRegistry())

	result, err := runner.Run(context.Background(), suite, Options{Seed: 1, Parallel: 1})
	if err != nil {
		t.Fatalf("Run returned: %v", err)
	}

	if len(result.Scenarios) != 1 || len(result.Scenarios[0].Steps) != 1 {
		t.Fatalf("expected one scenario with one step, got %+v", result)
	}

	step := result.Scenarios[0].Steps[0]
	if step.Status != "fail" {
		t.Fatalf("step status = %q, want fail", step.Status)
	}

	if step.Failure == nil {
		t.Fatalf("expected step failure detail")
	}

	if !strings.Contains(step.Failure.Message, "must be an integer") {
		t.Fatalf("unexpected failure message: %q", step.Failure.Message)
	}
}

func TestRunnerRejectsFractionalLoadRequests(t *testing.T) {
	t.Parallel()

	suite := loadTales(t, fractionalRequestsTales)

	runner := NewRunner(provider.NewRegistry())

	result, err := runner.Run(context.Background(), suite, Options{Seed: 1, Parallel: 1})
	if err != nil {
		t.Fatalf("Run returned: %v", err)
	}

	step := result.Scenarios[0].Steps[0]
	if step.Status != "fail" {
		t.Fatalf("step status = %q, want fail", step.Status)
	}

	if step.Failure == nil || !strings.Contains(step.Failure.Message, "must be an integer") {
		t.Fatalf("expected fractional-requests rejection, got %+v", step.Failure)
	}
}
