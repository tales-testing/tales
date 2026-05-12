package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	appledriver "github.com/hyperxlab/tales/drivers/apple"
	"github.com/hyperxlab/tales/internal/provider/mobile/apple"
	"github.com/hyperxlab/tales/internal/provider/mobile/apple/embeddeddriver"
	"github.com/hyperxlab/tales/internal/version"
	"github.com/urfave/cli/v2"
)

// NewDoctorCommand returns the `tales doctor` subcommand, which prints
// the embedded driver state, the apple-driver cache contents, and host
// Xcode / simctl diagnostics.
func NewDoctorCommand() *cli.Command {
	return &cli.Command{
		Name:  "doctor",
		Usage: "Inspect the embedded driver cache and host Xcode/simctl state",
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:  "json",
				Usage: "emit a single JSON object instead of human-readable text",
			},
		},
		Action: runDoctor,
	}
}

// Snapshot is the structured payload `tales doctor` produces. Field
// names are stable and consumable from CI scripts via --json.
type Snapshot struct {
	Tales          TalesInfo    `json:"tales"`
	EmbeddedDriver EmbeddedInfo `json:"embedded_driver"`
	Cache          CacheInfo    `json:"cache"`
	Xcode          XcodeInfo    `json:"xcode"`
	Simctl         SimctlInfo   `json:"simctl"`
}

// TalesInfo describes the running tales binary.
type TalesInfo struct {
	Version   string `json:"version"`
	GoVersion string `json:"go_version"`
	BuildDate string `json:"build_date,omitempty"`
	Platform  string `json:"platform"`
}

// EmbeddedInfo summarizes the embedded driver source.
type EmbeddedInfo struct {
	SourceHash      string `json:"source_hash"`
	SourceHashShort string `json:"source_hash_short"`
	Files           int    `json:"files"`
	Bytes           int64  `json:"bytes"`
	Error           string `json:"error,omitempty"`
}

// CacheInfo summarizes the apple-driver cache directory.
type CacheInfo struct {
	Base    string                      `json:"base"`
	Entries []embeddeddriver.CacheEntry `json:"entries"`
	Error   string                      `json:"error,omitempty"`
}

// XcodeInfo summarizes host Xcode state.
type XcodeInfo struct {
	Version      string `json:"version"`
	SDKVersion   string `json:"sdk_version"`
	DeveloperDir string `json:"developer_dir"`
	MacOSMajor   string `json:"macos_major"`
}

// SimctlInfo summarizes available simulator runtimes and devices.
type SimctlInfo struct {
	Available bool            `json:"available"`
	Runtimes  []SimctlRuntime `json:"runtimes"`
	Devices   []SimctlDevice  `json:"devices"`
	Error     string          `json:"error,omitempty"`
}

// SimctlRuntime mirrors the subset of `simctl list -j runtimes` Tales
// uses.
type SimctlRuntime struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	IsAvailable bool   `json:"is_available"`
}

// SimctlDevice mirrors the subset of `simctl list -j devices` Tales
// uses.
type SimctlDevice struct {
	UDID        string `json:"udid"`
	Name        string `json:"name"`
	Runtime     string `json:"runtime"`
	State       string `json:"state"`
	IsAvailable bool   `json:"is_available"`
}

func runDoctor(c *cli.Context) error {
	snapshot := collectSnapshot(c.Context)

	if c.Bool("json") {
		data, err := json.MarshalIndent(snapshot, "", "  ")
		if err != nil {
			return cli.Exit(fmt.Sprintf("marshal doctor snapshot: %v", err), 3)
		}

		if _, err := fmt.Fprintln(c.App.Writer, string(data)); err != nil {
			return cli.Exit(fmt.Sprintf("write doctor output: %v", err), 3)
		}

		return nil
	}

	if err := formatTextSnapshot(c.App.Writer, snapshot); err != nil {
		return cli.Exit(fmt.Sprintf("format doctor output: %v", err), 3)
	}

	return nil
}

func collectSnapshot(ctx context.Context) Snapshot {
	if ctx == nil {
		ctx = context.Background()
	}

	info := version.Get()
	snap := Snapshot{
		Tales: TalesInfo{
			Version:   info.Version,
			GoVersion: info.GoVersion,
			BuildDate: info.BuildDate,
			Platform:  info.Platform,
		},
	}

	snap.EmbeddedDriver = collectEmbeddedInfo()
	snap.Cache = collectCacheInfo()

	runner := apple.ExecRunner{}
	cmdRunner := commandRunnerAdapter{runner: runner}
	snap.Xcode = XcodeInfo{
		Version:      embeddeddriver.XcodeVersion(ctx, cmdRunner),
		SDKVersion:   embeddeddriver.XcodeSDKVersion(ctx, cmdRunner),
		DeveloperDir: embeddeddriver.DeveloperDir(ctx, cmdRunner),
		MacOSMajor:   embeddeddriver.MacOSMajor(ctx, cmdRunner),
	}

	snap.Simctl = collectSimctlInfo(ctx, runner)

	return snap
}

func collectEmbeddedInfo() EmbeddedInfo {
	hash, files, bytes, err := embeddeddriver.EmbeddedSourceStats(appledriver.FS(), appledriver.SourceRoot)
	if err != nil {
		return EmbeddedInfo{Error: err.Error()}
	}

	return EmbeddedInfo{
		SourceHash:      hash,
		SourceHashShort: embeddeddriver.EmbeddedShortHash(hash),
		Files:           files,
		Bytes:           bytes,
	}
}

func collectCacheInfo() CacheInfo {
	base, err := embeddeddriver.ResolveBase()
	if err != nil {
		return CacheInfo{Error: err.Error()}
	}

	info := CacheInfo{Base: base, Entries: []embeddeddriver.CacheEntry{}}

	entries, err := embeddeddriver.ListCacheEntries(base)
	if err != nil {
		info.Error = err.Error()

		return info
	}

	info.Entries = entries

	return info
}

// commandRunnerAdapter narrows apple.Runner (with its non-typed Run
// signature) to embeddeddriver.CommandRunner (which only needs the
// stdout bytes). Errors from the underlying runner propagate unchanged.
type commandRunnerAdapter struct {
	runner apple.Runner
}

func (a commandRunnerAdapter) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	out, err := a.runner.Run(ctx, name, args...)
	if err != nil {
		return out, fmt.Errorf("run %s: %w", name, err)
	}

	return out, nil
}

// rawSimctlRuntime / rawSimctlDevice are the camelCase shapes simctl
// emits. They are converted into the stable SimctlRuntime / SimctlDevice
// types used in the doctor snapshot output.
type rawSimctlRuntime struct {
	Identifier   string `json:"identifier"`
	Name         string `json:"name"`
	Version      string `json:"version"`
	BuildVersion string `json:"buildversion"`
	IsAvailable  bool   `json:"isAvailable"`
}

type rawSimctlDevice struct {
	UDID        string `json:"udid"`
	Name        string `json:"name"`
	State       string `json:"state"`
	IsAvailable bool   `json:"isAvailable"`
}

type simctlListJSON struct {
	Runtimes []rawSimctlRuntime           `json:"runtimes"`
	Devices  map[string][]rawSimctlDevice `json:"devices"`
}

func collectSimctlInfo(ctx context.Context, runner apple.Runner) SimctlInfo {
	out, err := runner.Run(ctx, "xcrun", "simctl", "list", "-j")
	if err != nil {
		return SimctlInfo{Available: false, Error: err.Error()}
	}

	var raw simctlListJSON
	if jsonErr := json.Unmarshal(out, &raw); jsonErr != nil {
		return SimctlInfo{Available: false, Error: fmt.Sprintf("decode simctl list: %v", jsonErr)}
	}

	info := SimctlInfo{
		Available: true,
		Runtimes:  make([]SimctlRuntime, 0, len(raw.Runtimes)),
		Devices:   make([]SimctlDevice, 0),
	}

	for _, r := range raw.Runtimes {
		display := r.Name
		if display == "" && r.Version != "" {
			display = "iOS " + r.Version
		}

		info.Runtimes = append(info.Runtimes, SimctlRuntime{
			ID:          r.Identifier,
			Name:        display,
			IsAvailable: r.IsAvailable,
		})
	}

	for runtimeID, devices := range raw.Devices {
		for _, d := range devices {
			info.Devices = append(info.Devices, SimctlDevice{
				UDID:        d.UDID,
				Name:        d.Name,
				Runtime:     runtimeID,
				State:       d.State,
				IsAvailable: d.IsAvailable,
			})
		}
	}

	// Stable ordering: runtimes by id, devices by name then udid.
	sort.Slice(info.Runtimes, func(i, j int) bool { return info.Runtimes[i].ID < info.Runtimes[j].ID })
	sort.Slice(info.Devices, func(i, j int) bool {
		if info.Devices[i].Name != info.Devices[j].Name {
			return info.Devices[i].Name < info.Devices[j].Name
		}

		return info.Devices[i].UDID < info.Devices[j].UDID
	})

	return info
}

func formatTextSnapshot(out io.Writer, snap Snapshot) error {
	if _, err := fmt.Fprintln(out, "== Tales =="); err != nil {
		return err //nolint:wrapcheck // direct passthrough of io.Writer error suffices.
	}

	writeKV(out, "version", snap.Tales.Version)
	writeKV(out, "go", snap.Tales.GoVersion)

	if snap.Tales.BuildDate != "" {
		writeKV(out, "built", snap.Tales.BuildDate)
	}

	writeKV(out, "platform", snap.Tales.Platform)

	fmt.Fprintln(out)
	fmt.Fprintln(out, "== Embedded driver ==")

	if snap.EmbeddedDriver.Error != "" {
		writeKV(out, "error", snap.EmbeddedDriver.Error)
	} else {
		writeKV(out, "source hash", snap.EmbeddedDriver.SourceHashShort)
		writeKV(out, "files", fmt.Sprintf("%d", snap.EmbeddedDriver.Files))
		writeKV(out, "bytes", fmt.Sprintf("%d", snap.EmbeddedDriver.Bytes))
	}

	fmt.Fprintln(out)
	fmt.Fprintln(out, "== Driver cache ==")

	writeKV(out, "base", snap.Cache.Base)

	if snap.Cache.Error != "" {
		writeKV(out, "error", snap.Cache.Error)
	}

	writeKV(out, "entries", fmt.Sprintf("%d", len(snap.Cache.Entries)))

	for i, entry := range snap.Cache.Entries {
		fmt.Fprintln(out)
		fmt.Fprintf(out, "  [%d] %s\n", i+1, entry.Key)
		writeIndentedKV(out, "source hash", formatEntryHash(entry, snap.EmbeddedDriver.SourceHash))
		writeIndentedKV(out, "extracted", boolToYesNo(entry.HasExtractOK))
		writeIndentedKV(out, "built", boolToBuiltDetail(entry))

		if entry.Metadata.XcodeVersion != "" {
			writeIndentedKV(out, "xcode", entry.Metadata.XcodeVersion)
		}

		if entry.Metadata.SDKVersion != "" {
			writeIndentedKV(out, "sdk", entry.Metadata.SDKVersion)
		}

		if entry.Metadata.IOSRuntime != "" {
			writeIndentedKV(out, "ios runtime", entry.Metadata.IOSRuntime)
		}

		if entry.Metadata.MacOSMajor != "" {
			writeIndentedKV(out, "macos", entry.Metadata.MacOSMajor)
		}

		if entry.Metadata.CreatedAt != "" {
			writeIndentedKV(out, "created", entry.Metadata.CreatedAt)
		}

		writeIndentedKV(out, "size", humanBytes(entry.SizeBytes))
		writeIndentedKV(out, "path", entry.Path)

		if entry.MetadataError != "" {
			writeIndentedKV(out, "metadata error", entry.MetadataError)
		}
	}

	fmt.Fprintln(out)
	fmt.Fprintln(out, "== Xcode ==")

	writeKV(out, "xcodebuild", snap.Xcode.Version)
	writeKV(out, "sdk", snap.Xcode.SDKVersion+" (iphonesimulator)")
	writeKV(out, "developer", snap.Xcode.DeveloperDir)
	writeKV(out, "macos", snap.Xcode.MacOSMajor)

	fmt.Fprintln(out)
	fmt.Fprintln(out, "== simctl ==")

	if !snap.Simctl.Available {
		writeKV(out, "available", "no")

		if snap.Simctl.Error != "" {
			writeKV(out, "error", snap.Simctl.Error)
		}
	} else {
		writeKV(out, "runtimes", fmt.Sprintf("%d available", countAvailable(snap.Simctl.Runtimes)))
		writeKV(out, "devices", fmt.Sprintf("%d available", countAvailableDevices(snap.Simctl.Devices)))

		for _, r := range snap.Simctl.Runtimes {
			if !r.IsAvailable {
				continue
			}

			fmt.Fprintf(out, "  - %s (%s)\n", r.Name, r.ID)
		}

		for _, d := range snap.Simctl.Devices {
			if !d.IsAvailable {
				continue
			}

			fmt.Fprintf(out, "  - %s %s — %s — %s\n", d.Name, shortUDID(d.UDID), d.State, shortRuntime(d.Runtime))
		}
	}

	fmt.Fprintln(out)
	fmt.Fprintln(out, "== Hints ==")
	fmt.Fprintln(out, "  - wipe the cache:   make clean-ios-driver-cache  (or rm -rf <base>)")
	fmt.Fprintln(out, "  - override base:    TALES_DRIVER_CACHE_DIR=/some/path tales doctor")
	fmt.Fprintln(out, "  - stale simulator:  sudo xcodebuild -runFirstLaunch && xcrun simctl shutdown all")
	fmt.Fprintln(out)

	return nil
}

func writeKV(out io.Writer, key, value string) {
	fmt.Fprintf(out, "  %-12s %s\n", key+":", value)
}

func writeIndentedKV(out io.Writer, key, value string) {
	fmt.Fprintf(out, "      %-14s %s\n", key+":", value)
}

func boolToYesNo(b bool) string {
	if b {
		return "yes"
	}

	return "no"
}

func boolToBuiltDetail(entry embeddeddriver.CacheEntry) string {
	if !entry.HasBuildOK {
		return "no"
	}

	if entry.XCTestRunPath == "" {
		return "yes (build.ok present, xctestrun path unknown)"
	}

	return fmt.Sprintf("yes → %s", entry.XCTestRunPath)
}

func formatEntryHash(entry embeddeddriver.CacheEntry, embeddedHash string) string {
	hash := entry.Metadata.SourceHash
	if hash == "" {
		return "unknown"
	}

	short := embeddeddriver.EmbeddedShortHash(hash)
	if embeddedHash != "" && strings.HasPrefix(embeddedHash, hash) {
		return short + " (matches embedded ✓)"
	}

	if embeddedHash != "" && !strings.HasPrefix(embeddedHash, hash) {
		return short + " (⚠ source-hash mismatch — rebuilt on next run)"
	}

	return short
}

func humanBytes(b int64) string {
	const unit = 1024

	if b < unit {
		return fmt.Sprintf("%d B", b)
	}

	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}

	suffix := "KMGTPE"[exp]

	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), suffix)
}

func countAvailable(runtimes []SimctlRuntime) int {
	n := 0

	for _, r := range runtimes {
		if r.IsAvailable {
			n++
		}
	}

	return n
}

func countAvailableDevices(devices []SimctlDevice) int {
	n := 0

	for _, d := range devices {
		if d.IsAvailable {
			n++
		}
	}

	return n
}

// shortUDID renders only the leading characters of a simulator UDID so
// the doctor output stays compact.
func shortUDID(udid string) string {
	if len(udid) <= 8 {
		return "(" + udid + ")"
	}

	return "(" + udid[:8] + "…)"
}

// shortRuntime extracts the iOS-XX-Y portion of a runtime identifier
// (`com.apple.CoreSimulator.SimRuntime.iOS-18-0` → `iOS-18-0`).
func shortRuntime(runtime string) string {
	if idx := strings.LastIndex(runtime, "."); idx >= 0 && idx+1 < len(runtime) {
		return runtime[idx+1:]
	}

	return runtime
}
