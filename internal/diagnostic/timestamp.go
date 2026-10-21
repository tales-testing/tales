package diagnostic

import "time"

// TimestampLayout is the layout of every wall-clock time Tales writes into a
// report or a diagnostics artifact: UTC, millisecond precision, fixed width,
// e.g. "2026-10-21T09:54:34.123Z". One layout everywhere lets a reader line
// up an action of the JSONL report with the console and network entries a
// browser step recorded around it, by sorting or comparing the strings.
const TimestampLayout = "2006-01-02T15:04:05.000Z07:00"

// FormatTimestamp renders t in TimestampLayout, converted to UTC.
func FormatTimestamp(t time.Time) string {
	return t.UTC().Format(TimestampLayout)
}
