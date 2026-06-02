package runtime

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/zclconf/go-cty/cty"
)

func TestDateGeneratorUsesDeterministicFaker(t *testing.T) {
	t.Parallel()

	params := map[string]cty.Value{
		"from": cty.StringVal("1970-01-01"),
		"to":   cty.StringVal("2030-12-31"),
	}
	parts := []string{"scenario", "step", "request.body.json", "birth_date"}

	first, err := runGenerator("date", params, newGeneratorRandom(1234, parts...))
	if err != nil {
		t.Fatalf("generate first date: %v", err)
	}

	second, err := runGenerator("date", params, newGeneratorRandom(1234, parts...))
	if err != nil {
		t.Fatalf("generate second date: %v", err)
	}

	otherSeed, err := runGenerator("date", params, newGeneratorRandom(5678, parts...))
	if err != nil {
		t.Fatalf("generate other-seed date: %v", err)
	}

	if first.AsString() != second.AsString() {
		t.Fatalf("same seed generated different dates: %q vs %q", first.AsString(), second.AsString())
	}

	if first.AsString() == otherSeed.AsString() {
		t.Fatalf("different seed generated same date: %q", first.AsString())
	}

	if !regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`).MatchString(first.AsString()) {
		t.Fatalf("date is not formatted as YYYY-MM-DD: %q", first.AsString())
	}
}

func TestDateGeneratorRangeBounds(t *testing.T) {
	t.Parallel()

	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2024, 1, 10, 0, 0, 0, 0, time.UTC)
	params := map[string]cty.Value{
		"from": cty.StringVal("2024-01-01"),
		"to":   cty.StringVal("2024-01-10"),
	}

	for i := range 50 {
		value, err := runGenerator("date", params, newGeneratorRandom(int64(i), "scenario", "step", "request.body.json", "birth_date"))
		if err != nil {
			t.Fatalf("generate date: %v", err)
		}

		parsed, err := time.Parse(dateLayout, value.AsString())
		if err != nil {
			t.Fatalf("generated date is not parseable: %q", value.AsString())
		}

		if parsed.Before(from) || parsed.After(to) {
			t.Fatalf("generated date %q outside [%s, %s]", value.AsString(), from, to)
		}
	}
}

func TestDateGeneratorInclusiveSingleDay(t *testing.T) {
	t.Parallel()

	params := map[string]cty.Value{
		"from": cty.StringVal("2024-07-18"),
		"to":   cty.StringVal("2024-07-18"),
	}

	value, err := runGenerator("date", params, newGeneratorRandom(1234, "scenario", "step", "request.body.json", "birth_date"))
	if err != nil {
		t.Fatalf("generate date: %v", err)
	}

	if value.AsString() != "2024-07-18" {
		t.Fatalf("inclusive single-day range should return the exact date, got %q", value.AsString())
	}
}

func TestDateTimeGeneratorUsesDeterministicFaker(t *testing.T) {
	t.Parallel()

	params := map[string]cty.Value{
		"from": cty.StringVal("1970-01-01T00:00:00Z"),
		"to":   cty.StringVal("2030-12-31T23:59:59Z"),
	}
	parts := []string{"scenario", "step", "request.body.json", "created_at"}

	first, err := runGenerator("datetime", params, newGeneratorRandom(1234, parts...))
	if err != nil {
		t.Fatalf("generate first datetime: %v", err)
	}

	second, err := runGenerator("datetime", params, newGeneratorRandom(1234, parts...))
	if err != nil {
		t.Fatalf("generate second datetime: %v", err)
	}

	otherSeed, err := runGenerator("datetime", params, newGeneratorRandom(5678, parts...))
	if err != nil {
		t.Fatalf("generate other-seed datetime: %v", err)
	}

	if first.AsString() != second.AsString() {
		t.Fatalf("same seed generated different datetimes: %q vs %q", first.AsString(), second.AsString())
	}

	if first.AsString() == otherSeed.AsString() {
		t.Fatalf("different seed generated same datetime: %q", first.AsString())
	}

	if !regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$`).MatchString(first.AsString()) {
		t.Fatalf("datetime is not RFC3339 UTC: %q", first.AsString())
	}
}

func TestDateTimeGeneratorRangeBoundsUTC(t *testing.T) {
	t.Parallel()

	from := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2024, 6, 2, 0, 0, 0, 0, time.UTC)
	params := map[string]cty.Value{
		"from": cty.StringVal("2024-06-01T00:00:00Z"),
		"to":   cty.StringVal("2024-06-02T00:00:00Z"),
	}

	for i := range 50 {
		value, err := runGenerator("datetime", params, newGeneratorRandom(int64(i), "scenario", "step", "request.body.json", "created_at"))
		if err != nil {
			t.Fatalf("generate datetime: %v", err)
		}

		parsed, err := time.Parse(time.RFC3339, value.AsString())
		if err != nil {
			t.Fatalf("generated datetime is not parseable: %q", value.AsString())
		}

		if !strings.HasSuffix(value.AsString(), "Z") {
			t.Fatalf("generated datetime should be UTC (Z suffix): %q", value.AsString())
		}

		if parsed.Before(from) || parsed.After(to) {
			t.Fatalf("generated datetime %q outside [%s, %s]", value.AsString(), from, to)
		}
	}
}

func TestUnixTimeGeneratorReturnsNumber(t *testing.T) {
	t.Parallel()

	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC).Unix()
	to := time.Date(2024, 12, 31, 23, 59, 59, 0, time.UTC).Unix()
	params := map[string]cty.Value{
		"from": cty.StringVal("2024-01-01T00:00:00Z"),
		"to":   cty.StringVal("2024-12-31T23:59:59Z"),
	}

	value, err := runGenerator("unix_time", params, newGeneratorRandom(1234, "scenario", "step", "request.body.json", "event_ts"))
	if err != nil {
		t.Fatalf("generate unix_time: %v", err)
	}

	if value.Type() != cty.Number {
		t.Fatalf("unix_time must return a number, got %s", value.Type().FriendlyName())
	}

	seconds, accuracy := value.AsBigFloat().Int64()
	if accuracy != 0 {
		t.Fatalf("unix_time must be an integer number of seconds, got %s", value.AsBigFloat().Text('f', -1))
	}

	if seconds < from || seconds > to {
		t.Fatalf("generated unix_time %d outside [%d, %d]", seconds, from, to)
	}
}

func TestUnixTimeGeneratorUsesDeterministicFaker(t *testing.T) {
	t.Parallel()

	params := map[string]cty.Value{
		"from": cty.StringVal("1970-01-01T00:00:00Z"),
		"to":   cty.StringVal("2030-12-31T23:59:59Z"),
	}
	parts := []string{"scenario", "step", "request.body.json", "event_ts"}

	first, err := runGenerator("unix_time", params, newGeneratorRandom(1234, parts...))
	if err != nil {
		t.Fatalf("generate first unix_time: %v", err)
	}

	second, err := runGenerator("unix_time", params, newGeneratorRandom(1234, parts...))
	if err != nil {
		t.Fatalf("generate second unix_time: %v", err)
	}

	otherSeed, err := runGenerator("unix_time", params, newGeneratorRandom(5678, parts...))
	if err != nil {
		t.Fatalf("generate other-seed unix_time: %v", err)
	}

	if !first.RawEquals(second) {
		t.Fatalf("same seed generated different unix_time values: %s vs %s", first.AsBigFloat().Text('f', -1), second.AsBigFloat().Text('f', -1))
	}

	if first.RawEquals(otherSeed) {
		t.Fatalf("different seed generated same unix_time value: %s", first.AsBigFloat().Text('f', -1))
	}
}

func TestUnixTimeGeneratorInclusiveSingleSecond(t *testing.T) {
	t.Parallel()

	want := time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC).Unix()
	params := map[string]cty.Value{
		"from": cty.StringVal("2024-06-15T12:00:00Z"),
		"to":   cty.StringVal("2024-06-15T12:00:00Z"),
	}

	value, err := runGenerator("unix_time", params, newGeneratorRandom(1234, "scenario", "step", "request.body.json", "event_ts"))
	if err != nil {
		t.Fatalf("generate unix_time: %v", err)
	}

	seconds, _ := value.AsBigFloat().Int64()
	if seconds != want {
		t.Fatalf("inclusive single-second range should return %d, got %d", want, seconds)
	}
}

func TestDateTimeGeneratorsInvalidConfigs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		generatorType string
		params        map[string]cty.Value
		want          string
	}{
		{
			name:          "date missing from",
			generatorType: "date",
			params:        map[string]cty.Value{"to": cty.StringVal("2006-12-31")},
			want:          "date generator from is required",
		},
		{
			name:          "date missing to",
			generatorType: "date",
			params:        map[string]cty.Value{"from": cty.StringVal("1980-01-01")},
			want:          "date generator to is required",
		},
		{
			name:          "date invalid from",
			generatorType: "date",
			params:        map[string]cty.Value{"from": cty.StringVal("not-a-date"), "to": cty.StringVal("2006-12-31")},
			want:          "date generator from must be a valid YYYY-MM-DD value",
		},
		{
			name:          "date invalid to",
			generatorType: "date",
			params:        map[string]cty.Value{"from": cty.StringVal("1980-01-01"), "to": cty.StringVal("12-31-2006")},
			want:          "date generator to must be a valid YYYY-MM-DD value",
		},
		{
			name:          "date from after to",
			generatorType: "date",
			params:        map[string]cty.Value{"from": cty.StringVal("2006-12-31"), "to": cty.StringVal("1980-01-01")},
			want:          "date generator from must be on or before to",
		},
		{
			name:          "datetime missing from",
			generatorType: "datetime",
			params:        map[string]cty.Value{"to": cty.StringVal("2024-12-31T23:59:59Z")},
			want:          "datetime generator from is required",
		},
		{
			name:          "datetime invalid from",
			generatorType: "datetime",
			params:        map[string]cty.Value{"from": cty.StringVal("2024-01-01"), "to": cty.StringVal("2024-12-31T23:59:59Z")},
			want:          "datetime generator from must be a valid RFC3339 value",
		},
		{
			name:          "datetime from after to",
			generatorType: "datetime",
			params:        map[string]cty.Value{"from": cty.StringVal("2024-12-31T23:59:59Z"), "to": cty.StringVal("2024-01-01T00:00:00Z")},
			want:          "datetime generator from must be on or before to",
		},
		{
			name:          "unix_time missing to",
			generatorType: "unix_time",
			params:        map[string]cty.Value{"from": cty.StringVal("2024-01-01T00:00:00Z")},
			want:          "unix_time generator to is required",
		},
		{
			name:          "unix_time invalid to",
			generatorType: "unix_time",
			params:        map[string]cty.Value{"from": cty.StringVal("2024-01-01T00:00:00Z"), "to": cty.StringVal("nope")},
			want:          "unix_time generator to must be a valid RFC3339 value",
		},
		{
			name:          "unix_time from after to",
			generatorType: "unix_time",
			params:        map[string]cty.Value{"from": cty.StringVal("2024-12-31T23:59:59Z"), "to": cty.StringVal("2024-01-01T00:00:00Z")},
			want:          "unix_time generator from must be on or before to",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := runGenerator(tt.generatorType, tt.params, newGeneratorRandom(1234, "scenario", "step", "request.body.json", "field"))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected %q error, got %v", tt.want, err)
			}
		})
	}
}
