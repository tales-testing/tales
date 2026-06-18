package parser

import (
	"strings"
	"testing"
)

func TestLoadPathScenarioRecordBasic(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "preview" {
  record {
    output  = "preview.mp4"
    codec   = "h264"
    mask    = "black"
    display = "internal"
    target  = "iphone15"
    force   = true
  }

  step "http" "ping" {
    request { url = "http://example.test/ping" }
  }
}
`

	suite, diags := LoadPath(writeTales(t, content))
	if diags.HasErrors() {
		t.Fatalf("unexpected diagnostics: %s", diags.Error())
	}

	scenario := suite.Scenarios[0]
	if scenario.Record == nil {
		t.Fatal("expected scenario.Record to be populated")
	}

	rec := scenario.Record
	if rec.Output.Empty() || rec.Codec.Empty() || rec.Mask.Empty() ||
		rec.Display.Empty() || rec.Target.Empty() || rec.Force.Empty() {
		t.Fatalf("expected every record attribute to be set, got %+v", rec)
	}

	if rec.Range.Start.Line == 0 {
		t.Fatal("expected record block range to be populated")
	}
}

func TestLoadPathScenarioRecordOptional(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "no-record" {
  step "http" "ping" {
    request { url = "http://example.test/ping" }
  }
}
`

	suite, diags := LoadPath(writeTales(t, content))
	if diags.HasErrors() {
		t.Fatalf("unexpected diagnostics: %s", diags.Error())
	}

	if suite.Scenarios[0].Record != nil {
		t.Fatalf("expected scenario.Record to be nil, got %+v", suite.Scenarios[0].Record)
	}
}

func TestLoadPathScenarioRecordRequiresOutput(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "preview" {
  record {
    codec = "h264"
  }

  step "http" "ping" {
    request { url = "http://example.test/ping" }
  }
}
`

	_, diags := LoadPath(writeTales(t, content))
	if !diags.HasErrors() {
		t.Fatal("expected missing output to be rejected")
	}

	if !strings.Contains(diags.Error(), "scenario record block must declare output") {
		t.Fatalf("unexpected diagnostics: %s", diags.Error())
	}
}

func TestLoadPathScenarioRecordOnlyOutput(t *testing.T) {
	t.Parallel()

	content := `version = 1

scenario "preview" {
  record {
    output = "preview.mp4"
  }

  step "http" "ping" {
    request { url = "http://example.test/ping" }
  }
}
`

	suite, diags := LoadPath(writeTales(t, content))
	if diags.HasErrors() {
		t.Fatalf("unexpected diagnostics: %s", diags.Error())
	}

	rec := suite.Scenarios[0].Record
	if rec == nil {
		t.Fatal("expected scenario.Record to be populated")
	}

	if rec.Output.Empty() {
		t.Fatal("expected output to be set")
	}

	if !rec.Codec.Empty() || !rec.Mask.Empty() || !rec.Display.Empty() ||
		!rec.Target.Empty() || !rec.Force.Empty() {
		t.Fatalf("expected optional fields to be empty, got %+v", rec)
	}
}
