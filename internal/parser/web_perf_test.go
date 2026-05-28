package parser

import (
	"strings"
	"testing"
)

func TestLoadPathBrowserWebPerfAliases(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "perf" {
  step "browser" "open" {
    target = "chrome"
    actions {
      goto {
        url = "https://example.com/"
      }
    }
    expect {
      web_perf {
        fcp                = lt("1800ms")
        lcp                = lt("2500ms")
        cls                = lt(0.1)
        load               = lt("3000ms")
        dom_content_loaded = lt("1500ms")
        resources_count    = gte(1)
      }
    }
  }
}
`

	suite, diags := LoadPath(writeTales(t, content))
	if diags.HasErrors() {
		t.Fatalf("unexpected diagnostics: %s", diags.Error())
	}

	step := suite.Scenarios[0].Steps[0]
	if step.Browser == nil {
		t.Fatal("expected browser step")
	}

	if got := len(step.Browser.Expect.WebPerf); got != 6 {
		t.Fatalf("WebPerf entries=%d want 6", got)
	}

	gotMetrics := map[string]bool{}
	for _, exp := range step.Browser.Expect.WebPerf {
		gotMetrics[exp.Metric] = true
	}

	for _, want := range []string{
		"fcp_ms", "lcp_ms", "cls",
		"load_event_ms", "dom_content_loaded_ms", "resources_count",
	} {
		if !gotMetrics[want] {
			t.Errorf("missing canonical metric %q in %v", want, gotMetrics)
		}
	}
}

func TestLoadPathBrowserWebPerfUnknownMetric(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "perf" {
  step "browser" "open" {
    target = "chrome"
    actions {
      goto {
        url = "https://example.com/"
      }
    }
    expect {
      web_perf {
        speed_index = lt(2000)
      }
    }
  }
}
`

	_, diags := LoadPath(writeTales(t, content))
	if !diags.HasErrors() {
		t.Fatalf("expected error for unknown metric")
	}

	if !strings.Contains(diags.Error(), "unknown web_perf metric") {
		t.Fatalf("unexpected error: %s", diags.Error())
	}
}

func TestLoadPathMobileRejectsWebPerf(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "perf" {
  step "mobile" "open" {
    target = "iphone"
    platform = "ios"
    actions {
      tap {
        id = "hello"
      }
    }
    expect {
      web_perf {
        fcp = lt("1s")
      }
    }
  }
}
`

	_, diags := LoadPath(writeTales(t, content))
	if !diags.HasErrors() {
		t.Fatalf("mobile must reject web_perf")
	}

	if !strings.Contains(diags.Error(), "web_perf expectation is browser-only") {
		t.Fatalf("unexpected error: %s", diags.Error())
	}
}
