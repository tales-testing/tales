package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/tales-testing/tales/internal/provider/mobile/apple/embeddeddriver"
)

func baseSnapshot() Snapshot {
	return Snapshot{
		Tales: TalesInfo{
			Version:   "0.1.0+abcdef0",
			GoVersion: "go1.26.0",
			BuildDate: "2026-05-12T18:00:00Z",
			Platform:  "darwin/arm64",
		},
		EmbeddedDriver: EmbeddedInfo{
			SourceHash:      "3a7f1c2e9d4b91ff0011223344556677889900aabbccddeeff",
			SourceHashShort: "3a7f1c2e9d4b91ff",
			Files:           9,
			Bytes:           59341,
		},
		Cache: CacheInfo{
			Base:    "/Users/eu/Library/Caches/tales/apple-driver",
			Entries: []embeddeddriver.CacheEntry{},
		},
		Xcode: XcodeInfo{
			Version:      "Xcode 26.5 Build version 17F42",
			SDKVersion:   "17.4",
			DeveloperDir: "/Applications/Xcode.app/Contents/Developer",
			MacOSMajor:   "14",
		},
		Simctl: SimctlInfo{Available: true},
	}
}

func render(t *testing.T, snap Snapshot) string {
	t.Helper()

	var buf bytes.Buffer
	if err := formatTextSnapshot(&buf, snap); err != nil {
		t.Fatalf("formatTextSnapshot: %v", err)
	}

	return buf.String()
}

func TestDoctorTextOutputBasicSections(t *testing.T) {
	t.Parallel()

	out := render(t, baseSnapshot())

	for _, want := range []string{
		"== Tales ==",
		"== Embedded driver ==",
		"== Driver cache ==",
		"== Xcode ==",
		"== simctl ==",
		"== Hints ==",
		"0.1.0+abcdef0",
		"darwin/arm64",
		"3a7f1c2e9d4b91ff",
		"entries:     0",
		"Xcode 26.5 Build version 17F42",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected output to contain %q, got:\n%s", want, out)
		}
	}
}

func TestDoctorTextOutputHealthyEntryShowsMatchMarker(t *testing.T) {
	t.Parallel()

	snap := baseSnapshot()
	snap.Cache.Entries = []embeddeddriver.CacheEntry{{
		Key:           "3a7f1c2e9d4b91ff-xcode-X",
		Path:          "/cache/3a7f1c2e9d4b91ff-xcode-X",
		HasExtractOK:  true,
		HasBuildOK:    true,
		XCTestRunPath: "/cache/3a7f1c2e9d4b91ff-xcode-X/derived-data/Build/Products/Driver.xctestrun",
		SizeBytes:     44040192,
		Metadata: embeddeddriver.Metadata{
			SourceHash:   "3a7f1c2e9d4b91ff",
			XcodeVersion: "Xcode 26.5",
			SDKVersion:   "17.4",
			IOSRuntime:   "iOS-18-0",
			MacOSMajor:   "14",
			CreatedAt:    "2026-05-12T19:42:00Z",
		},
	}}

	out := render(t, snap)

	for _, want := range []string{
		"matches embedded ✓",
		"yes → /cache/3a7f1c2e9d4b91ff-xcode-X/derived-data/Build/Products/Driver.xctestrun",
		"iOS-18-0",
		"2026-05-12T19:42:00Z",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output, got:\n%s", want, out)
		}
	}
}

func TestDoctorTextOutputMismatchedHashFlagged(t *testing.T) {
	t.Parallel()

	snap := baseSnapshot()
	snap.Cache.Entries = []embeddeddriver.CacheEntry{{
		Key:          "old-cache-key",
		Path:         "/cache/old-cache-key",
		HasExtractOK: true,
		HasBuildOK:   true,
		Metadata: embeddeddriver.Metadata{
			SourceHash: "deadbeefdeadbeef",
		},
	}}

	out := render(t, snap)
	if !strings.Contains(out, "⚠ source-hash mismatch") {
		t.Fatalf("expected mismatch marker, got:\n%s", out)
	}
}

func TestDoctorTextOutputBrokenEntryFlagged(t *testing.T) {
	t.Parallel()

	snap := baseSnapshot()
	snap.Cache.Entries = []embeddeddriver.CacheEntry{{
		Key:           "broken",
		Path:          "/cache/broken",
		HasExtractOK:  false,
		HasBuildOK:    false,
		MetadataError: "open metadata.json: no such file or directory",
	}}

	out := render(t, snap)

	for _, want := range []string{
		"source hash:   unknown",
		"extracted:     no",
		"built:         no",
		"metadata error",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in broken-entry output, got:\n%s", want, out)
		}
	}
}

func TestDoctorTextOutputXcodeUnknown(t *testing.T) {
	t.Parallel()

	snap := baseSnapshot()
	snap.Xcode = XcodeInfo{
		Version:      embeddeddriver.XcodeUnknown,
		SDKVersion:   embeddeddriver.XcodeUnknown,
		DeveloperDir: embeddeddriver.XcodeUnknown,
		MacOSMajor:   embeddeddriver.XcodeUnknown,
	}

	out := render(t, snap)
	if !strings.Contains(out, "xcodebuild:  unknown") {
		t.Fatalf("expected unknown Xcode marker, got:\n%s", out)
	}
}

func TestDoctorTextOutputSimctlUnavailable(t *testing.T) {
	t.Parallel()

	snap := baseSnapshot()
	snap.Simctl = SimctlInfo{Available: false, Error: "exit status 72: simctl boom"}

	out := render(t, snap)
	if !strings.Contains(out, "available:   no") {
		t.Fatalf("expected simctl unavailable marker, got:\n%s", out)
	}

	if !strings.Contains(out, "simctl boom") {
		t.Fatalf("expected simctl error in output, got:\n%s", out)
	}
}

func TestDoctorTextOutputSimctlListsRuntimesAndDevices(t *testing.T) {
	t.Parallel()

	snap := baseSnapshot()
	snap.Simctl = SimctlInfo{
		Available: true,
		Runtimes: []SimctlRuntime{
			{ID: "com.apple.CoreSimulator.SimRuntime.iOS-18-0", Name: "iOS 18.0", IsAvailable: true},
			{ID: "com.apple.CoreSimulator.SimRuntime.iOS-17-5", Name: "iOS 17.5", IsAvailable: false},
		},
		Devices: []SimctlDevice{
			{UDID: "9F2C1234567890ABCDEF", Name: "iPhone 17", Runtime: "com.apple.CoreSimulator.SimRuntime.iOS-18-0", State: "Shutdown", IsAvailable: true},
			{UDID: "STALEUDID", Name: "Old iPhone", Runtime: "com.apple.CoreSimulator.SimRuntime.iOS-15-0", State: "Shutdown", IsAvailable: false},
		},
	}

	out := render(t, snap)

	for _, want := range []string{
		"runtimes:    1 available",
		"devices:     1 available",
		"iOS 18.0",
		"iPhone 17",
		"iOS-18-0",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in simctl output, got:\n%s", want, out)
		}
	}

	if strings.Contains(out, "Old iPhone") || strings.Contains(out, "iOS 17.5") {
		t.Errorf("expected unavailable runtimes/devices to be filtered, got:\n%s", out)
	}
}

func TestDoctorJSONRoundTrip(t *testing.T) {
	t.Parallel()

	in := baseSnapshot()
	in.Cache.Entries = []embeddeddriver.CacheEntry{{
		Key:           "k1",
		Path:          "/cache/k1",
		HasExtractOK:  true,
		HasBuildOK:    true,
		XCTestRunPath: "/cache/k1/x.xctestrun",
		SizeBytes:     42,
		Metadata:      embeddeddriver.Metadata{SourceHash: "3a7f1c2e9d4b91ff"},
	}}

	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var out Snapshot
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if out.Tales.Version != in.Tales.Version {
		t.Errorf("version mismatch: %q vs %q", out.Tales.Version, in.Tales.Version)
	}

	if len(out.Cache.Entries) != 1 {
		t.Fatalf("expected 1 cache entry after round-trip, got %d", len(out.Cache.Entries))
	}

	if out.Cache.Entries[0].XCTestRunPath != "/cache/k1/x.xctestrun" {
		t.Errorf("xctestrun mismatch: %q", out.Cache.Entries[0].XCTestRunPath)
	}

	if out.EmbeddedDriver.SourceHash != in.EmbeddedDriver.SourceHash {
		t.Errorf("source hash mismatch: %q vs %q", out.EmbeddedDriver.SourceHash, in.EmbeddedDriver.SourceHash)
	}
}

func TestHumanBytes(t *testing.T) {
	t.Parallel()

	cases := map[int64]string{
		0:                  "0 B",
		512:                "512 B",
		1024:               "1.0 KiB",
		1536:               "1.5 KiB",
		1024 * 1024:        "1.0 MiB",
		1024 * 1024 * 1024: "1.0 GiB",
	}

	for in, want := range cases {
		if got := humanBytes(in); got != want {
			t.Errorf("humanBytes(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestShortRuntime(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"com.apple.CoreSimulator.SimRuntime.iOS-18-0": "iOS-18-0",
		"iOS-17-0": "iOS-17-0",
		"":         "",
	}

	for in, want := range cases {
		if got := shortRuntime(in); got != want {
			t.Errorf("shortRuntime(%q) = %q, want %q", in, got, want)
		}
	}
}
