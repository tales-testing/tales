package driver

import (
	"testing"
	"time"
)

func TestPointStringRoundsToWholePixels(t *testing.T) {
	t.Parallel()

	cases := []struct {
		point Point
		want  string
	}{
		{Point{X: 912, Y: 684}, "(912, 684)"},
		{Point{X: 911.5, Y: 683.49}, "(912, 683)"},
		{Point{X: 0.2, Y: -3.7}, "(0, -4)"},
	}

	for _, tc := range cases {
		if got := tc.point.String(); got != tc.want {
			t.Errorf("%+v.String() = %q, want %q", tc.point, got, tc.want)
		}
	}
}

func TestActionabilityErrorNamesTheFailingCheck(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  ActionabilityError
		want string
	}{
		{
			name: "covered, with the budget that expired",
			err: ActionabilityError{
				Action:   "click",
				Selector: "[data-testid='signup.submit']",
				Reason:   `point (912, 684) is covered by <div class="toast">`,
				Point:    &Point{X: 912, Y: 684},
				Timeout:  2 * time.Second,
			},
			want: `click [data-testid='signup.submit']: not actionable after 2s: point (912, 684) is covered by <div class="toast">`,
		},
		{
			name: "a budget measured a few microseconds late still reads round",
			err: ActionabilityError{
				Action:   "hover",
				Selector: "#menu",
				Reason:   "element is not stable",
				Timeout:  1999987 * time.Microsecond,
			},
			want: "hover #menu: not actionable after 2s: element is not stable",
		},
		{
			name: "no deadline",
			err: ActionabilityError{
				Action:   "check",
				Selector: "#tos",
				Reason:   "element is disabled",
			},
			want: "check #tos: not actionable: element is disabled",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := tc.err.Error(); got != tc.want {
				t.Errorf("Error() = %q\nwant      %q", got, tc.want)
			}
		})
	}
}
