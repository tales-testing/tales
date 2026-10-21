package diagnostic

import (
	"testing"
	"time"
)

func TestFormatTimestampIsUTCWithFixedMilliseconds(t *testing.T) {
	t.Parallel()

	paris := time.FixedZone("CEST", 2*60*60)

	tests := []struct {
		name string
		in   time.Time
		want string
	}{
		{name: "converted to UTC", in: time.Date(2026, 10, 21, 11, 54, 34, 123_456_789, paris), want: "2026-10-21T09:54:34.123Z"},
		{name: "whole second keeps three digits", in: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), want: "2026-01-02T03:04:05.000Z"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := FormatTimestamp(tt.in); got != tt.want {
				t.Errorf("FormatTimestamp() = %q, want %q", got, tt.want)
			}
		})
	}
}
