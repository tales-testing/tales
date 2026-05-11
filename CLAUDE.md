# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Tales is a single-binary integration / E2E testing tool (Go 1.26). Tests are written as declarative HCL2 files with the `.tales` extension and executed by the `tales` CLI. Inspired by Robot Framework and Karate, focused on HTTP API workflows with deterministic, seedable test data generation.

## Common Commands

```bash
make build           # Build tales + mockserver into ./build
make test            # Unit tests with -race -count=1 across ./internal/... and ./cmd/tales
make lint            # golangci-lint (config in .golangci.yml — v2 schema)
make e2e             # Build + start mock server + run ./e2e/pass with --seed 1234 --parallel 4
make e2e-failure     # Run ./e2e/fail and assert CLI exits with code 1
make install         # Install tales binary to $GOBIN / $GOPATH/bin
make install-skill   # Copy .claude/skills/tales-test-generator to ~/.claude/skills
```

Run a single Go test:

```bash
go test -race -run TestName ./internal/runtime
```

Run the CLI directly during development:

```bash
go run ./cmd/tales test ./e2e/pass --seed 1234
go run ./cmd/tales validate ./e2e/pass/blog.tales
```

CLI exit codes are load-bearing for CI: `0` pass, `1` failure, `2` parse/validation error, `3` runtime/reporting fatal — match these when adding new exit paths in [internal/cli/test.go](internal/cli/test.go).

## Architecture

Pipeline: HCL files → parser → model → runtime → providers → reporters.

- [cmd/tales/main.go](cmd/tales/main.go) — urfave/cli entrypoint, wires `test` and `validate` subcommands.
- [internal/cli/](internal/cli/) — command flags/handlers. `test.go` constructs the provider registry (`http`, `keyword`) and invokes the runner.
- [internal/parser/](internal/parser/) — HCL2 loading. `schema.go` defines the gohcl struct tags that drive the DSL surface; `decode.go` converts those into `model.*`; `loader.go` walks `.tales` files, merges suites, and validates uniqueness of scenario/step/generator/keyword names. **Editing the DSL almost always means changing `schema.go` + `decode.go` together.**
- [internal/model/](internal/model/) — plain data structures (`Suite`, `Scenario`, `Step`, `Request`, `Expect`, `Keyword`, `Generator`). `Expression` wraps `hcl.Expression` with file/line metadata for diagnostics.
- [internal/lang/](internal/lang/) — expression evaluation on top of go-cty. `functions.go` registers built-ins and matchers (matchers are encoded as objects carrying a `__tales_matcher` key, then resolved by the assertion engine). `refs.go` extracts cross-step dependencies from expressions for the DAG.
- [internal/dag/](internal/dag/) — topological layering of steps; runner executes each layer in parallel.
- [internal/runtime/](internal/runtime/) — execution engine.
  - `runner.go` is the orchestrator: scenario-level parallelism via semaphore, per-scenario DAG layering, retry loop, teardown is always run after main steps (even on failure).
  - `generator.go` + `seed.go` — deterministic generation. Each `generate()` call mixes (seed, scenario name, step name, generator name, expression path) so identical runs produce identical values regardless of parallelism or retry attempt.
  - `keyword.go` (under provider/keyword) — `keyword` blocks are reusable flows with their own inputs/steps/outputs; called via `step "keyword" "name" { name = "...", inputs = {...} }`.
- [internal/assertion/](internal/assertion/) — JSON / status / header matcher logic, consuming the matcher objects produced by `lang/functions.go`.
- [internal/provider/](internal/provider/) — pluggable execution backends.
  - `http/provider.go` — HTTP provider (including Connect JSON over HTTP). Supports `request.body { json | form | raw }`, `request.auth.basic`. Rejects combining `headers.Authorization` with `auth.basic`.
  - `keyword/` — pseudo-provider that invokes user-defined keyword flows.
- [internal/report/](internal/report/) — console (ANSI), JUnit XML, and JSONL writers. `SuiteResult.Failed()` drives the CLI exit code.
- [internal/diagnostic/](internal/diagnostic/) — error formatting helpers shared by parser and runtime.
- [e2e/mockserver/](e2e/mockserver/) — small in-memory HTTP API used by all E2E suites; started by `make e2e` on port 1337.

## DSL Surface (when editing it)

The DSL accepts these top-level blocks: `version`, `config`, `generator "<type>" "<name>"`, `scenario "<name>"`, `keyword "<name>"`. Inside `scenario`: `step "<provider>" "<name>"` (with optional `retry`, `request`, `expect`, `capture`, `depends_on`, `when`) and `teardown { step ... }`. Backward-compatible aliases — `case` for `step`, `response` for `expect` — are decoded in [internal/parser/schema.go](internal/parser/schema.go) and must keep working.

Available generator types: `email`, `password`, `timezone`, `locale`, `person`, `mac_address`, `bytes`. Available matchers/functions: `env`, `generate`, `jsonencode`, `url_encode`, `regex_find`, `contains`, `matches`, `exists`, `not_exists`, `is_string`, `is_number`, `is_bool`, `is_array`, `is_object`, `one_of`, `can`. The README and the `tales-test-generator` skill carry the user-facing reference; keep all three in sync when adding to this list.

## Working With `.tales` Test Files

For generating or maintaining `.tales` suites use the `tales-test-generator` skill (defined in [.claude/skills/tales-test-generator/SKILL.md](.claude/skills/tales-test-generator/SKILL.md)). Canonical examples live in [e2e/pass/](e2e/pass/); intentionally-failing fixtures in [e2e/fail/](e2e/fail/) are used by `make e2e-failure` and must continue to fail with exit code 1.

## Conventions

- `golangci-lint` v2 config — notable enabled linters include `wsl_v5`, `nlreturn`, `wrapcheck`, `gocyclo` (min-complexity 18), `forcetypeassert`. Run `make lint` before sending changes; CI uses golangci-lint-action v9 with version `v2.10.1`.
- Tests use `-race -count=1`. Avoid time-based flakes — the runtime's retry/scheduler code paths are exercised under race.
- Determinism is a load-bearing property: never introduce wall-clock or `rand` calls in generators/runner without threading the existing seed mixer in [internal/runtime/seed.go](internal/runtime/seed.go).

## Working Rules (mandatory)

### Language

- Always reply to the user in **French**.
- All code, comments, identifiers, commit messages, and documentation must be in **English**.

### Testing

- Every new feature MUST ship with both:
  - **Unit tests** in the relevant `internal/...` package (`*_test.go` with `-race`).
  - **Functional tests** as `.tales` scenarios under `e2e/pass/` (and `e2e/fail/` when the feature has an error path worth pinning).
- Run `make test` and `make e2e` after each feature — do not declare a task done until both pass.
- Follow **TDD for bug fixes**: first write a failing unit or `.tales` test that reproduces the bug, confirm it fails for the right reason, then implement the fix and re-run the test to prove it passes.

### Linting

- Code must pass `make lint` (golangci-lint v2 config). Do not commit lint-dirty code.
- Never silence linters with `//nolint:...` directives. The only acceptable exception is a confirmed false positive, and it MUST carry an inline justification, e.g.:
  ```go
  //nolint:gosec // G404: deterministic seed required for reproducible test data, not a security primitive
  ```

### Docs and skill sync

When changing the DSL, CLI flags, generators, matchers, or any user-facing behavior, update **all three** in the same change:

- [README.md](README.md)
- [.claude/skills/tales-test-generator/SKILL.md](.claude/skills/tales-test-generator/SKILL.md) (and its `references/`)
- [CLAUDE.md](CLAUDE.md) if the architecture or conventions section is affected

### Git workflow

- **Commit at every step** of the work — one logical change per commit, not one big terminal commit.
- Commit messages MUST use Conventional Commits prefixes: `feat:`, `fix:`, `refactor:`, `test:`, `docs:`, `chore:`, `build:`, `ci:`, `perf:`, `style:`.
- Write **detailed** commit bodies when the change is non-trivial: what changed, why, and any user-visible impact. Subject stays under ~70 chars; details go in the body.
- **Never push.** The user pushes manually after review. Do not run `git push` (or `gh pr create`) unless explicitly asked in the current turn.
