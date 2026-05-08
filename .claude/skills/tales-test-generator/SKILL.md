---
name: tales-test-generator
description: Generate and maintain reliable .tales API test suites for Tales projects. Use when creating new API scenarios, keywords, teardown cleanup, or updating tests after Tales DSL/runtime changes.
---

# Tales Test Generator

## Goal

Produce valid, deterministic, and runnable `.tales` test suites for APIs.

## When to use

Use this skill when asked to:
- create a new `.tales` suite or scenario for an API flow
- refactor existing `.tales` tests while preserving behavior
- add reusable `keyword` flows
- update tests after Tales syntax/runtime evolution

## Required workflow

1. Read the local DSL source of truth before writing:
- `README.md`
- `internal/parser/schema.go`
- `internal/lang/functions.go`
- `internal/runtime/runner.go`
- `e2e/pass/*.tales`

2. Build tests with only supported structures:
- `version = 1`
- optional `config { ... }`
- optional `generator "type" "name" { ... }`
- `scenario "..." { ... }`
- `step "http" "name" { request { ... } expect { ... } capture { ... } }`
- optional `teardown { step ... }`
- optional `keyword "..." { inputs { ... } step ... outputs { ... } }`

3. Enforce reliability conventions:
- One business action per step.
- Stable step names in `snake_case`.
- Always assert at least `expect.status`.
- Prefer robust assertions (`contains`, `is_string`, `one_of`) over brittle full payload equality unless strict matching is explicitly required.
- Capture only reusable values needed by later steps/teardown.
- Use `result.<step>.<field>` references to model dependencies.

4. Always include resilient cleanup:
- Put cleanup calls in `teardown`.
- Guard destructive cleanup with `when = can(result.<creator_step>.<id_field>)`.
- For deletion status, prefer `one_of([200, 204, 404])` when API semantics allow idempotent cleanup.

5. Validate after generation:
- `tales validate <path>`
- `tales test <path> --seed 1234`
- if runtime supports it in the project context: add `--parallel` run to expose dependency issues

6. Fix and re-run until green:
- parse errors: align fields/blocks with parser schema
- eval errors: fix bad references (`result.*`, `request.*`, `response.*`, `input.*`)
- provider errors: fix method/url/headers/body types
- assertion errors: align matcher strategy with actual API contracts

## Tales-specific guardrails

- `step "keyword" "<name>"` must define:
  - `name = "<keyword_name>"`
  - `inputs = { ... }` when keyword inputs are required
- Avoid step name collisions:
  - unique names across scenario steps and teardown steps
  - keyword internal step names must not collide with outer scenario step names
- Use `request.json` for JSON payloads and `request.body` for non-JSON text payloads.
- `request.url` must be an absolute `http` or `https` URL.
- Matchers available: `contains`, `matches`, `exists`, `not_exists`, `is_string`, `is_number`, `is_bool`, `is_array`, `is_object`, `one_of`, `can`.

## Output contract

When asked to generate tests, produce:
- the final `.tales` file content
- a short validation command block to run
- any assumptions (base URL, auth prerequisites, cleanup semantics)

## Maintenance mode (keep up with Tales evolution)

When updating existing tests or creating new ones in a changed Tales codebase:
1. Re-read `README.md`, parser/runtime source files, and `e2e/pass/*.tales`.
2. Detect newly added/renamed fields, blocks, or matchers.
3. Update generated patterns to the current DSL and runtime behavior.
4. Re-validate using `tales validate` and `tales test`.

For canonical patterns and a starter template, read:
- [references/validation-checklist.md](references/validation-checklist.md)
- [references/api-test-template.tales](references/api-test-template.tales)
