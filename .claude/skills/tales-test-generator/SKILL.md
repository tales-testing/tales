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
- `internal/parser/mobile.go` (mobile DSL surface)
- `internal/model/mobile.go` (mobile model types)
- `internal/lang/functions.go`
- `internal/runtime/runner.go`
- `internal/runtime/mobile.go` (mobile execution + capture functions)
- `docs/mobile/ios.md` (mobile architecture and config)
- `e2e/pass/*.tales` (HTTP examples)
- `e2e/ios/pass/*.tales` (mobile examples)

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
- Use `request.body { json = ... }`, `request.body { form = ... }`, or `request.body { raw = ... }` for request bodies.
- Use `request.body.form` for `application/x-www-form-urlencoded` payloads; Tales URL-encodes form values and sets `Content-Type` when it is absent.
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
- Matchers/functions available: `contains`, `matches`, `exists`, `not_exists`, `is_string`, `is_number`, `is_bool`, `is_array`, `is_object`, `one_of`, `can`, `regex_find`, `url_encode`.
- Use `regex_find(value, pattern)` for full matches and `regex_find(value, pattern, group)` for capture groups.

## Mobile provider (iOS V1)

Architecture and runtime details live in
[docs/mobile/ios.md](../../../docs/mobile/ios.md). The skill focuses on
the DSL shape user-authored `.tales` files need.

### Targets configuration

Mobile targets must be declared inside `config.mobile.targets` and referenced
by name from each mobile step:

```hcl
config {
  mobile = {
    targets = {
      iphone = {
        platform    = "ios"
        device_name = env("IOS_DEVICE_NAME", "iPhone 17")
        app         = env("IOS_APP_PATH")
        bundle_id   = env("IOS_BUNDLE_ID", "com.hyperxlab.tales.demo")
        driver = {
          host = env("IOS_DRIVER_HOST", "127.0.0.1")
          port = 9080
        }
      }
    }
  }
}
```

- `platform` only accepts `"ios"` in V1.
- `app` must be an iOS Simulator `.app` bundle, not a device build.
- `driver.host` and `driver.port` are the only fields user-authored suites
  need. Tales extracts and builds the embedded XCUITest driver on first
  run.

### Step shape

```hcl
step "mobile" "<name>" {
  platform = "ios"
  target   = "<targets key>"

  launch    { clear_state = true }    # optional, mutually exclusive with terminate
  terminate {}                         # optional, ends the app session

  actions {
    # tap | input_text | clear_text | wait_visible | wait_not_visible
    # decoded in source order — order matters
  }

  expect {
    # visible | not_visible | text | value | enabled | disabled
  }

  capture {
    # value("id"), text("id"), or request.actions[N].value
  }
}
```

### Supported actions

All five actions accept optional `timeout` and `interval` (Go duration
strings such as `"2s"`, `"250ms"`). Implicit defaults are `10s` timeout with
`250ms` polling.

- `tap { id = "..." }`
- `input_text { id = "..." value = "..." secure = true }` — `secure = true`
  masks the value in console / JUnit / JSONL reports.
- `clear_text { id = "..." }`
- `wait_visible { id = "..." }` — explicit wait until the element is visible.
- `wait_not_visible { id = "..." }` — explicit wait until the element is gone.

Prefer `wait_visible` / `wait_not_visible` as the canonical way to bridge
asynchronous UI transitions; never insert sleeps.

### Supported expectations

All expectations accept optional `timeout` and `interval` (defaults `10s` /
`250ms`).

- `visible    { id = "..." }`
- `not_visible{ id = "..." }`
- `text       { id = "..." value = "..." }` — value may be a literal string or a
  matcher (`contains(...)`, `matches(...)`).
- `value      { id = "..." value = "..." }` — same matcher support as `text`.
- `enabled    { id = "..." }`
- `disabled   { id = "..." }`

### Captures

Inside `capture { ... }` of a mobile step the runtime injects two helper
functions that close over the recorded UI hierarchy:

- `value("id")` — returns the text/value of an element.
- `text("id")`  — returns the visible text of an element.

`request.actions[N].value` exposes the evaluated value of action `N` (zero-based),
useful for re-using a generated email or password downstream:

```hcl
capture {
  email = value("register.email")
}
```

### Conventions and guardrails

- Selectors are accessibility identifiers only — never visible text.
- Pin every screen entry with at least one `visible { id = "..." }` to avoid
  racing UI transitions.
- Use `wait_visible` / `wait_not_visible` inside `actions` to chain dependent
  taps and inputs in a single step.
- For end-of-scenario cleanup, put `terminate {}` inside `teardown` and guard
  with `when = true` (or `when = can(...)` when the launch step is conditional).
- Two scenarios cannot target the same mobile `target` in parallel — the
  runtime serializes them by name. Use distinct targets when parallelism is
  required.
- On failure Tales writes
  `build/artifacts/mobile/<scenario>-<hash>/<step>/<phase>/attempt-<n>/{screenshot.png,hierarchy.json}`
  and surfaces them in reports — do not encode these paths into the suite.
- Validation: prefer `tales test ./suite --seed 1234 --parallel 1` for mobile
  suites; bump parallelism only when targets are distinct.

### Mobile step skeleton

```hcl
step "mobile" "submit_register" {
  depends_on = ["open_register"]

  platform = "ios"
  target   = "iphone"

  actions {
    input_text {
      id    = "register.email"
      value = generate("ios_email")
    }
    input_text {
      id     = "register.password"
      value  = generate("ios_password")
      secure = true
    }
    tap { id = "register.submit" }
    wait_not_visible { id = "register.loading"; timeout = "5s" }
    wait_visible     { id = "verify.screen";   timeout = "10s" }
  }

  expect {
    enabled { id = "verify.submit" }
    value {
      id    = "register.email"
      value = contains("@example.com")
    }
  }

  capture {
    email = value("register.email")
  }
}
```

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

For canonical patterns and starter templates, read:
- [references/validation-checklist.md](references/validation-checklist.md)
- [references/api-test-template.tales](references/api-test-template.tales)
- [references/mobile-test-template.tales](references/mobile-test-template.tales)
