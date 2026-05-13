# Visual HTML Report

The visual report is a single offline HTML file that lets you replay a Tales
run action by action, screenshot by screenshot. It is designed for mobile
runs today; the data model is shaped so a future web/browser provider can
slot in without changes.

## Quick start

```bash
tales test ./e2e/ios/pass \
  --seed 1234 \
  --report-html build/reports/visual.html \
  --capture-screenshots actions
```

Open `build/reports/visual.html` in any modern browser. The file is fully
offline — no CDN, no network requests, no npm.

## CLI flags

| Flag | Description |
| --- | --- |
| `--report-html <path>` | Write the visual HTML report to `<path>`. Parent directory is created if missing. |
| `--capture-screenshots <mode>` | Pick the screenshot capture mode. One of `none`, `failures`, `steps`, `actions`. |

**Default capture mode**:

| Flags passed | Effective mode |
| --- | --- |
| neither | `failures` (legacy behavior) |
| `--report-html` only | `actions` |
| explicit `--capture-screenshots` | always wins |

Invalid values exit with code 2 and a message listing the four supported
modes.

## Layout

The HTML follows the brief literally — a macOS-Setup aesthetic:

- dark gradient backdrop, centered light card with soft shadow
- screenshot pane on the left
- vertical action timeline on the right, with the active action centered and
  highlighted, previous actions above, future actions below
- playback controls below: previous / play-pause / next, speed selector
  (0.5×, 0.75×, 1×, 1.5×, 2×), progress bar
- header has a scenario selector and a step selector plus an overall status
  pill

Keyboard shortcuts:

| Key | Action |
| --- | --- |
| `Space` | Play / pause |
| `→` | Next action |
| `←` | Previous action |
| `Home` | First action |
| `End` | Last action |

Playback timing is derived from each action's measured duration, clamped to
`[500ms, 3000ms]` and divided by the current speed factor. Long real waits
(`wait_visible` with a 10s timeout) therefore show their real duration in
the action label but are scrubbed quickly during playback.

## Artifact layout

When a capture mode writes artifacts, they are nested under the existing
mobile artifacts tree so screenshots stay grouped with the failure
artifacts they belong to:

```
build/artifacts/mobile/
  <scenario>-<hash>/
    <step>/
      <phase>/
        attempt-<n>/
          actions/
            0000-tap-welcome.register/
              screenshot.png
              hierarchy.json
            0001-input_text-register.email/
              screenshot.png
              hierarchy.json
          step/
            screenshot.png       # CaptureSteps mode only
            hierarchy.json
          screenshot.png         # legacy step-level failure
          hierarchy.json
```

Don't commit `build/artifacts/` — the directory is already gitignored.

## Modes

### `none`

No screenshot or hierarchy is ever written. Step-level failure artifacts
that previously appeared on failure are **also suppressed**. The only
artifact still surfaced is the `driver_log` produced when the embedded
driver fails to start, because it is the only way to debug a non-starting
driver.

### `failures` (default)

Matches the pre-visual-report behavior: one screenshot and one hierarchy
written at the step level on failure. Per-action results are still recorded
in the data model (with no screenshot paths) so JSONL action events and the
HTML timeline can still list what was queued.

### `steps`

One screenshot and one hierarchy captured at the end of each step.
Internally a synthetic `step_end` `ActionResult` is appended after the real
actions; the visual timeline renders one extra tile per step.

### `actions`

One screenshot and one hierarchy captured after each UI action succeeds,
plus a best-effort capture on the failing action. This is the mode that
makes the replay feel like a video. Used automatically when `--report-html`
is provided.

## Secure values

The brief's hard requirement: secure inputs must never leak.

- `input_text` actions marked `secure = true` are masked to `"***"` at one
  single boundary inside the mobile provider, before the value enters the
  `ActionResult`.
- All downstream consumers read the already-masked value: the HTML data
  island, the JSONL `action` events, the console summary.
- The driver still receives the plaintext (otherwise typing wouldn't work),
  but `hierarchy.json` does not echo password fields by default — verify
  for any new field you add.

`scripts/verify-ios-visual.sh` greps the rendered HTML for a list of
known-demo secrets (`hunter2`, `Secret123`) and fails the build if any
leaks. Add new canaries to that list when introducing new demo secrets.

## Performance notes

Per-action capture in `actions` mode adds a `Screenshot()` and a
`Hierarchy()` call to every UI action. On a slow simulator this can add
several seconds per scenario. Recommendations:

- CI: stick to `failures` unless you specifically want the visual replay.
- Local debugging: run `--capture-screenshots actions` only when needed.
- Capture errors never mask the underlying action error: they land in
  `step.Response["capture_warning"]` (if present) and the action's own
  result is reported faithfully.

## Known limitations (v1)

- Screenshot-based, not real video. No MP4 generation.
- No visual diff between runs.
- Mobile provider only. HTTP and keyword steps appear as plain entries with
  no screenshots. A web/browser provider is planned.
- Assets are external files by default (no base64 embedding). Moving the
  HTML file without moving `build/artifacts/` breaks image references —
  copy the artifacts tree alongside.
- Relative-path conversion falls back to the absolute path if `filepath.Rel`
  fails (cross-volume on Windows). The report still works locally but loses
  portability.

## Integration with other reporters

- **Console**: prints `HTML report: <path>` after writing.
- **JSONL** (`--report-jsonl`): emits one `"type":"action"` event per action
  after the step event when `step.Actions` is populated. Empty action slices
  produce byte-identical JSONL to the pre-visual-report format.
- **JUnit** (`--report-junit`): unchanged. Action-level data is intentionally
  not flattened into JUnit XML.
