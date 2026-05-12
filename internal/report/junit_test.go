package report

import (
	"strings"
	"testing"
)

func TestRenderArtifactsForJUnitUsesFallbackLabel(t *testing.T) {
	t.Parallel()

	got := renderArtifactsForJUnit([]Artifact{{Path: "build/artifacts/mobile/screenshot.png"}})
	if !strings.Contains(got, "artifact: build/artifacts/mobile/screenshot.png") {
		t.Fatalf("expected fallback artifact label, got %q", got)
	}
}
