package runtime

import (
	"strings"
	"testing"
)

func TestResolveMessageIDDeterministic(t *testing.T) {
	t.Parallel()

	a := resolveMessageID(nil, 1234, "Scenario", "send")
	b := resolveMessageID(nil, 1234, "Scenario", "send")

	if a != b {
		t.Fatalf("same seed/scenario/step must derive the same Message-ID: %q vs %q", a, b)
	}

	if !strings.HasPrefix(a, "<") || !strings.HasSuffix(a, "@tales.local>") {
		t.Fatalf("derived Message-ID has unexpected shape: %q", a)
	}
}

func TestResolveMessageIDVariesByStepAndScenario(t *testing.T) {
	t.Parallel()

	base := resolveMessageID(nil, 1234, "Scenario", "send")

	if resolveMessageID(nil, 1234, "Scenario", "other") == base {
		t.Fatalf("different step names must derive different Message-IDs")
	}

	if resolveMessageID(nil, 1234, "Other", "send") == base {
		t.Fatalf("different scenarios must derive different Message-IDs")
	}

	if resolveMessageID(nil, 9999, "Scenario", "send") == base {
		t.Fatalf("different seeds must derive different Message-IDs")
	}
}

func TestResolveMessageIDUserOverride(t *testing.T) {
	t.Parallel()

	headers := map[string]string{"Message-Id": "<user@example.com>"}

	if got := resolveMessageID(headers, 1234, "Scenario", "send"); got != "<user@example.com>" {
		t.Fatalf("user-supplied Message-ID must win (case-insensitive), got %q", got)
	}
}

func TestResolveMessageIDIgnoresEmptyOverride(t *testing.T) {
	t.Parallel()

	headers := map[string]string{"Message-ID": "   "}

	got := resolveMessageID(headers, 1234, "Scenario", "send")
	if got == "   " || !strings.HasSuffix(got, "@tales.local>") {
		t.Fatalf("blank Message-ID header must fall back to a derived id, got %q", got)
	}
}
