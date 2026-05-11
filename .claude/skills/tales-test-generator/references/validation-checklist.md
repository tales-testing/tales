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

## 4) Command validation

```bash
tales validate <path>
tales test <path> --seed 1234
```

Optional stress run:

```bash
tales test <path> --seed 1234 --parallel 4
```
