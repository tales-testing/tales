# Tales Validation Checklist

Run this checklist before considering a `.tales` suite done.

## 1) Structural validity

- File ends with `.tales`
- `version = 1` is present
- Every scenario has unique name
- Step names are unique across main steps + teardown steps
- Every step has a provider and name labels

## 2) Runtime safety

- Every HTTP step has `request.url` as absolute `http(s)` URL
- Every expected business-critical call has `expect.status`
- Raw response-body checks use `expect.body` when the contract is text-oriented
- `result.<step>` references point to existing step names
- `teardown` uses `when = can(...)` guards when a prerequisite can be missing

## 3) Determinism and resilience

- Dynamic values use `generator` + `generate("...")` when possible
- Assertions are not over-specified (prefer semantic matchers)
- Cleanup uses tolerant status where API supports idempotency (`one_of([200, 204, 404])`)
- Eventually consistent checks use bounded `retry` blocks, not sleeps
- `retry.attempts` is `>= 1` and `retry.interval` is a valid duration string
- Extracted tokens/codes use `regex_find(...)` with an explicit capture group when useful
- HTTP response header captures use `response.headers["Header-Name"]`; lower-case lookup should also work for HTTP responses
- Do not use `coalesce(...)` unless the local runtime explicitly documents lazy fallback support
- For protobuf/ConnectRPC payloads that may omit default-valued fields (`""`, `0`, `[]`, unspecified enums), use `optional(...)` around the expected value; reserve `required(...)` as an explicit readability wrapper and `any()` for "must be present, any value"

## 4) Mobile (iOS) specifics

When the suite contains `step "mobile"` blocks:

- `config.mobile.targets.<name>` is defined and `platform = "ios"`
- Each mobile step sets `platform = "ios"` and `target = "<targets key>"`
- Selectors use accessibility identifiers only, never visible text
- Every screen entry pins state with at least one `visible { id = "..." }` or `wait_visible`
- UI transitions use `wait_visible` / `wait_not_visible` rather than sleeps
- Sensitive `input_text` values are flagged with `secure = true`
- `terminate {}` lives in `teardown` and uses `when = true` (or `when = can(...)` when launch is conditional)
- Parallel scenarios target distinct mobile targets — the runtime serializes per-target work
- `text` / `value` expectations use literals or matchers (`contains`, `matches`), not over-specified equality
- `tales test ./suite --seed 1234 --parallel 1` is the safe default; only raise `--parallel` when targets are distinct

## 5) Command validation

```bash
tales validate <path>
tales test <path> --seed 1234
```

Optional stress run:

```bash
tales test <path> --seed 1234 --parallel 4
```
