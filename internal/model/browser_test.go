package model

import "testing"

func TestBrowserStepHasContent(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		step *BrowserStep
		want bool
	}{
		{"nil", nil, false},
		{"empty", &BrowserStep{}, false},
		{"with action", &BrowserStep{Actions: []BrowserAction{{Kind: BrowserActionGoto}}}, true},
		{"with visible", &BrowserStep{Expect: BrowserExpect{Visible: []BrowserVisibility{{}}}}, true},
		{"with attribute", &BrowserStep{Expect: BrowserExpect{Attribute: []BrowserAttributeExpectation{{}}}}, true},
		{"with url", &BrowserStep{Expect: BrowserExpect{URL: []BrowserURLExpectation{{}}}}, true},
		{"with title", &BrowserStep{Expect: BrowserExpect{Title: []BrowserTitleExpectation{{}}}}, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := tc.step.HasContent(); got != tc.want {
				t.Fatalf("HasContent() = %v, want %v", got, tc.want)
			}
		})
	}
}
