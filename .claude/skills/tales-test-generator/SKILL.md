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
- `step "http" "name" { optional retry { ... } request { ... } expect { ... } capture { ... } }`
- optional `teardown { step ... }`
- optional `keyword "..." { inputs { ... } step ... outputs { ... } }`

3. Enforce reliability conventions:
- One business action per step.
- Stable step names in `snake_case`.
- Always assert at least `expect.status`.
- Prefer robust assertions (`contains`, `is_string`, `one_of`) over brittle full payload equality unless strict matching is explicitly required.
- Capture only reusable values needed by later steps/teardown.
- Use `result.<step>.<field>` references to model dependencies.
- Use bounded `retry` blocks for eventually consistent HTTP flows instead of adding artificial sleeps.

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
- Use `request.auth.basic` for HTTP Basic Authentication instead of manually setting an `Authorization` header.
- Do not combine `headers.Authorization` with `auth.basic`; Tales rejects that conflict.
- Prefer `generator "password"` + `generate("...")` over hard-coded reusable passwords when creating users.
- Password generator options are `length`, `min_upper`, `min_lower`, `min_digit`, `min_special`, and `specials`.
- Faker-backed generators currently available: `email`, `password`, `timezone`, `locale`, `person`, `mac_address`, and `bytes`.
- Locale generator options are `language`, `country`, and `separator`.
- Person generator option is `gender` (`any`, `male`, or `female`) and it returns an object with `first_name`, `last_name`, `gender`, and `name`.
- MAC address generator options are `prefix`, `separator`, `lowercase`, and `uppercase`.
- Bytes generator options are `length` and `encoding` (`hex` or `base64`).
- `expect.body` can be used for raw response-body assertions, including matchers such as `contains(...)`.
- Response headers are accessible by key, for example `response.headers["Content-Type"]`; lower-case lookup such as `response.headers["content-type"]` is supported for HTTP responses.
- `retry.attempts` must be `>= 1`.
- `retry.interval` must be a valid Go duration string such as `"100ms"`, `"500ms"`, or `"2s"`.
- Matchers/functions available: `contains`, `matches`, `exists`, `not_exists`, `is_string`, `is_number`, `is_bool`, `is_array`, `is_object`, `one_of`, `can`, `regex_find`.
- Use `regex_find(value, pattern)` for full matches and `regex_find(value, pattern, group)` for capture groups.
- `coalesce(...)` is intentionally not a supported fallback primitive yet because lazy evaluation is required for missing-reference fallbacks.

## Async polling pattern

For asynchronous API effects such as email verification, prefer an HTTP polling step with `retry` and capture extracted data with `regex_find`:

```hcl
step "http" "find_verification_email" {
  retry {
    attempts = 10
    interval = "100ms"
  }

  request {
    method = "GET"
    url    = "${config.base_url}/mail/messages?to=${result.register.email}"
  }

  expect {
    status = 200
    body   = contains(result.register.email)
  }

  capture {
    code = regex_find(response.body, "verification code is ([A-Z0-9]{6})", 1)
  }
}
```

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
