package parser

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hyperxlab/tales/internal/model"
)

func writeTalesFile(t *testing.T, content string) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "skip.tales")

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write tales file: %s", err)
	}

	return dir
}

func TestLoadPathSkipScenarioBlocks(t *testing.T) {
	t.Parallel()

	dir := writeTalesFile(t, `version = 1

scenario "demo" {
  skip_unless {
    os      = ["darwin"]
    env_set = ["IOS_APP_PATH"]
    reason  = "iOS tests require macOS and IOS_APP_PATH"
  }

  skip_if {
    env = {
      SKIP_DEMO = "1"
    }
  }

  step "http" "ping" {
    request {
      method = "GET"
      url    = "http://example.test"
    }
  }
}
`)

	suite, diags := LoadPath(dir)
	if diags.HasErrors() {
		t.Fatalf("unexpected diagnostics: %s", diags.Error())
	}

	if len(suite.Scenarios) != 1 {
		t.Fatalf("want 1 scenario got %d", len(suite.Scenarios))
	}

	sc := suite.Scenarios[0]
	if len(sc.SkipRules) != 2 {
		t.Fatalf("want 2 skip rules got %d", len(sc.SkipRules))
	}

	// Rule decoding contract: skip_if rules are emitted before skip_unless rules.
	var skipIf, skipUnless *model.SkipRule

	for i := range sc.SkipRules {
		switch sc.SkipRules[i].Kind {
		case model.SkipIf:
			skipIf = &sc.SkipRules[i]
		case model.SkipUnless:
			skipUnless = &sc.SkipRules[i]
		}
	}

	if skipUnless == nil {
		t.Fatalf("skip_unless rule missing")
	}

	if skipUnless.OS.Empty() || skipUnless.EnvSet.Empty() || skipUnless.Reason.Empty() {
		t.Fatalf("skip_unless attrs not decoded: os=%v env_set=%v reason=%v",
			skipUnless.OS.Empty(), skipUnless.EnvSet.Empty(), skipUnless.Reason.Empty())
	}

	if skipIf == nil {
		t.Fatalf("skip_if rule missing")
	}

	if skipIf.Env.Empty() {
		t.Fatalf("skip_if env attr not decoded")
	}

	if sc.SkipRules[0].Kind != model.SkipIf {
		t.Fatalf("first rule kind=%s want skip_if (skip_if must be emitted before skip_unless)", sc.SkipRules[0].Kind)
	}
}

func TestLoadPathSkipStepBlocks(t *testing.T) {
	t.Parallel()

	dir := writeTalesFile(t, `version = 1

scenario "demo" {
  step "http" "debug" {
    skip_unless {
      condition = env("ENABLE_DEBUG", "0") == "1"
      reason    = "Set ENABLE_DEBUG=1 to run"
    }

    request {
      method = "GET"
      url    = "http://example.test/debug"
    }
  }
}
`)

	suite, diags := LoadPath(dir)
	if diags.HasErrors() {
		t.Fatalf("unexpected diagnostics: %s", diags.Error())
	}

	step := suite.Scenarios[0].Steps[0]
	if len(step.SkipRules) != 1 {
		t.Fatalf("want 1 skip rule on step got %d", len(step.SkipRules))
	}

	rule := step.SkipRules[0]
	if rule.Kind != model.SkipUnless {
		t.Fatalf("rule kind=%s want skip_unless", rule.Kind)
	}

	if rule.Condition.Empty() || rule.Reason.Empty() {
		t.Fatalf("step skip rule attrs not decoded")
	}
}

func TestLoadPathSkipBlockWithoutConditionsFails(t *testing.T) {
	t.Parallel()

	dir := writeTalesFile(t, `version = 1

scenario "demo" {
  skip_unless {
    reason = "no condition declared"
  }

  step "http" "ping" {
    request {
      method = "GET"
      url    = "http://example.test"
    }
  }
}
`)

	_, diags := LoadPath(dir)
	if !diags.HasErrors() {
		t.Fatalf("expected diagnostics for empty skip block")
	}

	found := false

	for _, d := range diags {
		if d.Summary == "Empty skip_unless block" {
			found = true

			break
		}
	}

	if !found {
		t.Fatalf("expected 'Empty skip_unless block' diagnostic, got: %s", diags.Error())
	}
}

func TestLoadPathSkipBlockUnknownAttrFails(t *testing.T) {
	t.Parallel()

	dir := writeTalesFile(t, `version = 1

scenario "demo" {
  skip_if {
    not_a_real_field = ["darwin"]
  }

  step "http" "ping" {
    request {
      method = "GET"
      url    = "http://example.test"
    }
  }
}
`)

	_, diags := LoadPath(dir)
	if !diags.HasErrors() {
		t.Fatalf("expected diagnostics for unknown skip attribute")
	}
}
