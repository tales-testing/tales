# Tales

Tales is a single-binary integration and end-to-end testing tool inspired by Robot Framework and Karate.

Scenarios are written in declarative HCL2 files with the `.tales` extension.

## Why Tales

- Single Go binary, easy to run in CI.
- Declarative DSL focused on API workflows.
- Deterministic seeded data generation.
- Scenarios run in parallel; steps within a scenario run sequentially in file order.
- Built-in HTTP provider (including ConnectRPC JSON over HTTP).
- Built-in SQL provider (`step "sql"`, PostgreSQL + MySQL) for preparing
  internal state that is not exposed through the public API. See
  [docs/providers/sql.md](docs/providers/sql.md).
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

## Release

Tagged releases (`v*`) are built by [the release workflow](.github/workflows/release.yml)
and published to GitHub Releases. Pre-built binaries are provided for
`linux/{amd64,arm64}` and `darwin/{amd64,arm64}`, with a `checksums.txt`
file alongside. See [docs/release.md](docs/release.md) for the release
process and how to verify a downloaded binary.

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
- `step "sql" "name" { connection = "<name>"; exec { ... } | query { ... } }`
  — see [docs/providers/sql.md](docs/providers/sql.md).
- `request.body { json = ... }` for JSON payloads.
- `request.body { form = ... }` for `application/x-www-form-urlencoded` payloads.
- `request.body { raw = ... }` for raw string payloads.
- `request.body { multipart { file { ... } field { ... } } }` for
  `multipart/form-data` uploads. File parts read from `path` (relative
  to the `.tales` file) or from an inline `content` expression; the
  `Content-Type` header is set automatically with the generated
  boundary. See *Multipart file upload* below.
- `request.auth.basic` for HTTP Basic Authentication.
- `vars { ... }` to declare step-local variables evaluated once before the
  provider runs — required for signing a request body (compute the body
  string and its HMAC once, send the same bytes). See *Step-local vars*
  below.
- `expect` assertions for status/headers/json.
- `capture` to expose a stable contract for next steps.
- `result.<step_name>.<field>` for cross-step references.
- `generator "email"`, `generator "password"`, `generator "timezone"`, `generator "locale"`, `generator "person"`, `generator "mac_address"`, and `generator "bytes"` for deterministic test data.
- `teardown { ... }` for deterministic cleanup.
- `keyword \"...\" { ... }` for reusable flows.
- `skip_if { ... }` / `skip_unless { ... }` on a scenario or step to gate execution on OS, architecture, env vars, or any HCL expression. See [docs/skip.md](docs/skip.md).

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

## Multipart file upload

`request.body { multipart { ... } }` builds a `multipart/form-data`
payload from an ordered list of `file { ... }` and `field { ... }`
children. Declaration order is preserved on the wire so signatures or
hashes computed over the body remain stable. `file` parts read their
bytes from `path` (a string resolved relative to the `.tales` file) or
from an inline `content` expression; the `Content-Type` request header
is set automatically with the boundary `mime/multipart` generated.

```hcl
generator "bytes" "attachment_blob" {
  length   = 32
  encoding = "hex"
}

step "http" "upload" {
  request {
    method = "POST"
    url    = "${config.base_url}/upload"

    body {
      multipart {
        file {
          field        = "avatar"
          path         = "./avatar.txt"          # relative to this .tales
          content_type = "text/plain"
        }
        file {
          field        = "attachment"
          content      = generate("attachment_blob")
          filename     = "attachment.bin"
          content_type = "application/octet-stream"
        }
        field {
          name  = "description"
          value = "fixture upload"
        }
      }
    }
  }
}
```

Rules:

- Each `file` block must declare **exactly one** of `path` or `content`.
  Both `filename` and `content_type` are optional; when omitted, the
  provider derives `filename` from `path` (or falls back to the field
  name) and sniffs `content_type` from the extension or payload.
- Each `field` block requires `name` and `value`.
- The `multipart` block cannot be combined with `json`, `form`, or
  `raw` — `body` must declare exactly one transport.
- Paths are resolved at runtime relative to the `.tales` file owning
  the step, so fixtures sit naturally next to the scenario.

A worked end-to-end example lives in
[e2e/pass/file_upload.tales](e2e/pass/file_upload.tales) (uses the
`/upload` mockserver endpoint, which hashes each part with SHA-256 so
the scenario can pin the exact wire payload).

## Step-local vars

A `vars { ... }` block declared inside a `step` introduces variables that
are evaluated **once**, in **declaration order**, **before** the provider
runs. Later vars can read earlier ones via `vars.<name>`; the cumulative
value is then visible to the step's `request`, `expect`, and `capture`
expressions through the `vars` scope variable.

Use it whenever a value must be stable across multiple interpolation sites
in the same step — most commonly when signing a request: the timestamp,
the canonical JSON body, and the HMAC over both must be computed exactly
once.

```hcl
step "http" "send_webhook" {
  vars {
    ts   = now_unix()
    body = jsonencode({
      id   = "evt-${result.create.id}"
      type = "notarization.completed"
    })
    sig  = hmac_sha256_hex(config.webhook_secret, "${vars.ts}.${vars.body}")
  }

  request {
    method = "POST"
    url    = "${config.api_base}/webhook"

    headers = {
      Content-Type    = "application/json"
      X-Signature     = "t=${vars.ts},v1=${vars.sig}"
    }

    body {
      raw = vars.body
    }
  }
}
```

Rules:

- vars are **step-local**. They are not visible to other steps — use
  `capture` to share a value with later steps.
- vars are **immutable** after evaluation.
- A var can only reference vars declared **earlier in the same block**.
  Forward references and self-references are rejected at load time
  (exit code `2`).
- `when` and `skip_if` / `skip_unless` are evaluated *before* the step
  body, so they cannot reference `vars.<name>` — that is rejected at
  load time with a clear error.
- The same `vars.<name>` must hold the same value at every interpolation
  site. This is guaranteed precisely because `vars` are evaluated once.

## Execution Model

- **Scenarios** run in parallel, up to `--parallel` at a time (default `1`).
- **Steps inside a scenario** run **sequentially, in the order they are defined**
  in the `.tales` file. There is no implicit parallelism between steps.
- A step may reference (`result.<step>`) or `depends_on` only steps defined
  **earlier** in the file. A forward reference or an unknown reference is
  rejected at load time — `tales validate` catches it and the exit code is `2`.
- `depends_on` is **optional**: file order already determines execution order.
  Use it only as explicit documentation/validation of a relationship; it does
  not reorder steps.
- When a step fails, the scenario stops: later steps are reported as skipped
  and are not executed.
- `teardown` steps run sequentially in file order, after the scenario's steps,
  even when a step failed.

## Built-in Functions and Matchers

General:

- `env(name)`
- `env(name, default)`
- `generate(name)`
- `jsonencode(value)` — serializes any value to a canonical JSON string.
  Object keys are sorted alphabetically; sets are sorted by their JSON
  encoding; numbers preserve precision. The deterministic output is what
  makes it safe to sign — two calls on the same value produce the same
  bytes.
- `url_encode(value)`
- `now_unix()` — current Unix timestamp in seconds as a number. Uses the
  wall clock (non-deterministic). Capture in a `vars { ts = now_unix() }`
  block to reuse the same value at every interpolation site.
- `now_rfc3339()` — current UTC time formatted per RFC3339, e.g.
  `"2026-05-26T15:42:31Z"`. Same caveats as `now_unix()`.
- `hmac_sha256_hex(secret, message)` — HMAC-SHA256 returned as a lowercase
  hex string. Pair with `jsonencode` and step-local `vars` to sign a
  request body that the server can re-verify byte for byte.

Top-level expression variables:

- `host.os` — `runtime.GOOS` (e.g. `"darwin"`, `"linux"`, `"windows"`)
- `host.arch` — `runtime.GOARCH` (e.g. `"amd64"`, `"arm64"`)
- `config.<key>`, `result.<step>.<field>`, plus `request`, `response`, `input` in step scope
- `vars.<name>` — step-local variables declared in the step's `vars {}`
  block. Only visible inside the step that declares them. See *Step-local
  vars* above.

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
- `optional(value)` — field-level matcher: passes when the key is absent, or
  when present and the inner expectation matches.
- `required(value)` — field-level matcher: explicit version of the default
  behavior. Fails when the key is absent, otherwise delegates to the inner
  expectation. Useful for readability when paired with `optional(...)`.
- `any()` — value matcher: matches any present value (`null`, string, number,
  bool, array, object). Does **not** make the field optional by itself —
  combine with `optional(any())` to also accept omitted keys.

### Optional fields (ConnectRPC / protobuf JSON)

ConnectRPC and protobuf JSON often omit fields holding their default value
(unspecified enums, empty arrays, empty strings, ...). Tales remains
strict-by-default: a field declared in `expect.json` must be present.
Wrap the expected value with `optional(...)` to allow the response to omit it:

```hcl
expect {
  status = 200

  json = {
    id           = required(is_string())
    role         = optional("ROLE_UNSPECIFIED")  # absent or equal to default
    permissions  = optional([])                  # absent or empty array
    display_name = optional("")                  # absent or empty string
    metadata     = optional(any())               # absent or any value
  }
}
```

Semantics:

- Object matching stays partial: extra fields on the actual response are
  ignored unless `strict = true`.
- Fields are required by default. `required(...)` is a no-op wrapper that
  exists for readability when other fields in the same block use
  `optional(...)`.
- `optional(expected)` passes when the key is absent. When the key is present
  the actual value must match `expected`.
- `optional("")` does **not** match an actual `null`. Use `optional(null)` to
  accept either `null` or a missing key.
- `any()` alone still requires the key to be present. Use `optional(any())`
  to also accept omitted keys.

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
- `internal/runtime`: execution engine, sequential step runner, seed logic.
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
