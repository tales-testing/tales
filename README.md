# Tales

Tales is a single-binary integration and end-to-end testing tool inspired by Robot Framework and Karate.

Scenarios are written in declarative HCL2 files with the `.tales` extension.

## Why Tales

- Single Go binary, easy to run in CI.
- Declarative DSL focused on API workflows.
- Deterministic seeded data generation.
- Scenario and step execution with dependency-aware scheduling.
- Built-in HTTP provider (including ConnectRPC JSON over HTTP).
- Native iOS UI automation via XCUITest (`step "mobile"`), no Appium / no
  Maestro at runtime. The XCUITest driver is **embedded** in the `tales`
  binary and built on first use into `~/Library/Caches/tales/apple-driver/`,
  so a released binary runs iOS tests on any macOS+Xcode host without the
  repository on disk. `tales doctor` (`--json` for CI) inspects the cache,
  embedded source, Xcode, and simctl state in one place. See
  [docs/mobile/ios.md](docs/mobile/ios.md).
- Human-readable console output.
- JUnit XML and JSONL reports with artifact paths (screenshots, UI hierarchy).

## Current Status

This repository contains a pragmatic V1 focused on HTTP workflows:

- `scenario` + `step` execution model.
- `expect` assertions.
- `capture` for stable step outputs.
- `teardown` block always executed after main steps.
- `when = can(...)` support in teardown.
- executable `keyword` blocks with `inputs` and `outputs`.
- Parallel scenario execution (`--parallel`).
- Deterministic generation via `--seed`.

## Installation and Build

Requirements:

- Go `1.26` (see `go.mod`)
- `golangci-lint` for `make lint`

Build:

```bash
go build -o build/tales ./cmd/tales
```

Or:

```bash
make build
```

## Quick Start

Create a file like `tests/blog.tales`:

```hcl
version = 1

config {
  base_url = env("BASE_URL", "http://localhost:1337")
}

generator "email" "user_email" {
  prefix = "test-"
  domain = "example.com"
}

generator "password" "user_password" {
  length      = 16
  min_upper   = 1
  min_lower   = 1
  min_digit   = 1
  min_special = 1
  specials    = "!@#$%^&*"
}

generator "timezone" "user_timezone" {}

generator "locale" "user_locale" {
  separator = "-"
}

generator "person" "user_person" {
  gender = "female"
}

generator "mac_address" "device_mac" {
  prefix    = "aa:bb"
  separator = "-"
  lowercase = true
}

generator "bytes" "trace_id" {
  length   = 8
  encoding = "hex"
}

scenario "Create a blog post" {
  tags = ["demo"]

  step "http" "create_user" {
    request {
      method = "POST"
      url    = "${config.base_url}/users"

      headers = {
        Accept       = "application/json"
        Content-Type = "application/json"
      }

      body {
        json = {
          email    = generate("user_email")
          password = generate("user_password")
          timezone = generate("user_timezone")
          locale   = generate("user_locale")
          person   = generate("user_person")
          device   = {
            mac_address = generate("device_mac")
          }
          trace_id = generate("trace_id")
        }
      }
    }

    expect {
      status = 201
      json = {
        id    = is_string()
        email = request.body.json.email
      }
    }

    capture {
      id       = response.json.id
      email    = response.json.email
      password = request.body.json.password
    }
  }

  step "http" "auth_user" {
    request {
      method = "POST"
      url    = "${config.base_url}/auth"
      headers = {
        Content-Type = "application/json"
      }
      body {
        json = {
          email    = result.create_user.email
          password = result.create_user.password
        }
      }
    }

    expect {
      status = 200
      json = {
        access_token = is_string()
      }
    }

    capture {
      token = response.json.access_token
    }
  }

  teardown {
    step "http" "delete_user" {
      when = can(result.create_user.id)

      request {
        method = "DELETE"
        url    = "${config.base_url}/users/${result.create_user.id}"
        headers = {
          Authorization = "Bearer ${result.auth_user.token}"
        }
      }

      expect {
        status = one_of([200, 204, 404])
      }
    }
  }
}
```

Run:

```bash
BASE_URL=http://localhost:1337 ./build/tales test ./tests --seed 1234
```

## CLI

### Validate

```bash
tales validate <path>
```

Examples:

```bash
tales validate ./e2e
tales validate ./e2e/pass/blog.tales
```

### Test

```bash
tales test <path> [flags]
```

Flags:

- `--seed <int>`: deterministic seed (if omitted, current time is used).
- `--parallel <int>`: scenario-level parallelism.
- `--tag <tag>`: run only scenarios containing one of these tags (repeatable).
- `--scenario <name>`: run one scenario by exact name.
- `--report-junit <path>`: write JUnit XML.
- `--report-jsonl <path>`: write JSONL events.
- `--report-html <path>`: write a single-file visual HTML report (mobile screenshots replay).
- `--capture-screenshots <mode>`: mobile screenshot capture mode. One of `none`, `failures`, `steps`, `actions`. Defaults to `failures`, or `actions` when `--report-html` is set.

Examples:

```bash
tales test ./e2e/pass --seed 1234 --parallel 4
tales test ./e2e/pass --tag demo
tales test ./e2e/pass --scenario "Create a blog post"
tales test ./e2e/pass --report-junit build/reports/e2e.junit.xml --report-jsonl build/reports/e2e.jsonl
```

Exit codes:

- `0`: all scenarios passed.
- `1`: at least one scenario failed.
- `2`: parse/validation failure.
- `3`: runtime/reporting fatal error.

## DSL Highlights

- `scenario "..." { ... }`
- `step "http" "name" { ... }`
- `request.body { json = ... }` for JSON payloads.
- `request.body { form = ... }` for `application/x-www-form-urlencoded` payloads.
- `request.body { raw = ... }` for raw string payloads.
- `request.auth.basic` for HTTP Basic Authentication.
- `expect` assertions for status/headers/json.
- `capture` to expose a stable contract for next steps.
- `result.<step_name>.<field>` for cross-step references.
- `generator "email"`, `generator "password"`, `generator "timezone"`, `generator "locale"`, `generator "person"`, `generator "mac_address"`, and `generator "bytes"` for deterministic test data.
- `teardown { ... }` for deterministic cleanup.
- `keyword \"...\" { ... }` for reusable flows.

Backward-compatible aliases currently accepted:

- `case` as alias for `step`
- `response` as alias for `expect`

Request body examples:

```hcl
request {
  body {
    form = {
      grant_type = "password"
      username   = result.user.email
      password   = result.user.password
    }
  }
}
```

`body.form` values are encoded with `application/x-www-form-urlencoded` semantics, so characters such as `&`, `=`, `+`, `%`, `#`, and spaces are safe in generated values.

## Built-in Functions and Matchers

General:

- `env(name)`
- `env(name, default)`
- `generate(name)`
- `jsonencode(value)`
- `url_encode(value)`

Matchers:

- `contains(value)`
- `matches(regex)`
- `exists()`
- `not_exists()`
- `is_string()`
- `is_number()`
- `is_bool()`
- `is_array()`
- `is_object()`
- `one_of(values)`
- `can(expression)`

## Determinism

Generation is deterministic with `--seed` and stable derivation inputs.

Running the same suite with the same seed produces the same generated values, even with parallel execution.
Step retries reuse the same deterministic generation context, so generated values do not change between retry attempts.

Password generators default to a 16-character password with at least one uppercase letter, lowercase letter, digit, and special character. Supported password options are `length`, `min_upper`, `min_lower`, `min_digit`, `min_special`, and `specials`.
Locale generators support `language`, `country`, and `separator`. Timezone generators return IANA tzdb names or aliases.
Person generators return an object with `first_name`, `last_name`, `gender`, and `name`. MAC address generators support `prefix`, `separator`, `lowercase`, and `uppercase`. Bytes generators return deterministic encoded bytes and support `length` plus `encoding` (`hex` or `base64`).

## Reports

### Console

Default output includes:

- Per-scenario status and duration.
- Per-step and teardown status.
- Failure details with request/response summaries.
- Replay command including seed.

### JUnit XML

Use `--report-junit <path>` for CI systems expecting JUnit.

### JSONL

Use `--report-jsonl <path>` for lightweight tooling and LLM pipelines.

Each line is one event (`scenario`, `step`, or `action`) with fields like:

- `type`, `phase`, `status`
- `file`, `scenario`, `step`, `provider`
- `duration_ms`, `seed`
- `error` (when failing)

When per-action recording is on (any `--capture-screenshots` other than `none`), each step event is followed by one `"type":"action"` event per UI action. Action events carry `index`, `kind`, `label`, `selector_id`, `secure`, `value`, `status`, `duration_ms`, and optional `screenshot` / `hierarchy` paths. Secure values are masked to `"***"`.

### Visual HTML

Use `--report-html <path>` to produce a single offline HTML file that replays the mobile test action by action — screenshot on the left, vertical action timeline on the right with the active action highlighted, plus playback controls (Space, ←/→, speed selector). The file is self-contained: vanilla CSS + JS are inlined; screenshots are referenced by relative paths next to the report.

Picking a capture mode:

- `none` — no screenshots or hierarchy ever (failures included). `driver_log` artifact is still surfaced for non-starting drivers.
- `failures` — default. Step-level screenshot + hierarchy only when a step fails (legacy behavior).
- `steps` — one end-of-step screenshot per step.
- `actions` — one screenshot per UI action. Required for a usable visual replay; selected automatically when `--report-html` is set.

Per-action artifacts live under:

```
build/artifacts/mobile/<scenario>-<hash>/<step>/<phase>/attempt-<n>/actions/NNNN-<kind>-<id>/
```

See [docs/reports/visual.md](docs/reports/visual.md) for a full walk-through, security notes, and limitations.

## E2E and Mock Server

The repository includes:

- Passing suites: `./e2e/pass/*.tales`
- Intentional failure suite: `./e2e/fail/*.tales`
- Real mock HTTP server: `./e2e/mockserver`

Run all passing E2E suites:

```bash
make e2e
```

Run intentional-failure E2E suite (expects CLI exit code `1`):

```bash
make e2e-failure
```

## Developer Workflow

```bash
make test
make lint
make e2e
```

## Project Layout

- `cmd/tales`: CLI entrypoint.
- `internal/cli`: command wiring (`test`, `validate`).
- `internal/parser`: loading and HCL decoding.
- `internal/model`: suite/scenario/step models.
- `internal/lang`: expression evaluation and functions.
- `internal/runtime`: execution engine, scheduler, seed logic.
- `internal/dag`: dependency graph/topological layering.
- `internal/assertion`: matcher and JSON assertion logic.
- `internal/provider/http`: HTTP execution provider.
- `internal/report`: console/JUnit/JSONL reporting.
- `e2e/mockserver`: in-memory test API used by E2E.

## Current Limitations

- HTTP is the only production-ready external provider.
- No browser/mobile providers.
- No external plugin system.
- No dedicated ConnectRPC provider (Connect JSON works through HTTP).

## License

See `LICENSE.md`.
