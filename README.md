# Tales

Tales is a single-binary integration and end-to-end testing tool inspired by Robot Framework and Karate.

Scenarios are written in declarative HCL2 files with the `.tales` extension.

## Why Tales

- Single Go binary, easy to run in CI.
- Declarative DSL focused on API workflows.
- Deterministic seeded data generation.
- Scenario and step execution with dependency-aware scheduling.
- Built-in HTTP provider (including ConnectRPC JSON over HTTP).
- Human-readable console output.
- JUnit XML and JSONL reports.

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

      json = {
        email    = generate("user_email")
        password = generate("user_password")
      }
    }

    expect {
      status = 201
      json = {
        id    = is_string()
        email = request.json.email
      }
    }

    capture {
      id       = response.json.id
      email    = response.json.email
      password = request.json.password
    }
  }

  step "http" "auth_user" {
    request {
      method = "POST"
      url    = "${config.base_url}/auth"
      headers = {
        Content-Type = "application/json"
      }
      json = {
        email    = result.create_user.email
        password = result.create_user.password
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
- `request.json` for JSON body.
- `request.body` for non-JSON payloads.
- `expect` assertions for status/headers/json.
- `capture` to expose a stable contract for next steps.
- `result.<step_name>.<field>` for cross-step references.
- `generator "email"` and `generator "password"` for deterministic test data.
- `teardown { ... }` for deterministic cleanup.
- `keyword \"...\" { ... }` for reusable flows.

Backward-compatible aliases currently accepted:

- `case` as alias for `step`
- `response` as alias for `expect`

## Built-in Functions and Matchers

General:

- `env(name)`
- `env(name, default)`
- `generate(name)`
- `jsonencode(value)`

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

Each line is one event (`scenario` or `step`) with fields like:

- `type`, `phase`, `status`
- `file`, `scenario`, `step`, `provider`
- `duration_ms`, `seed`
- `error` (when failing)

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
