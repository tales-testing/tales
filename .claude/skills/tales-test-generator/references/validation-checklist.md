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
- `result.<step>` references point to existing step names
- `teardown` uses `when = can(...)` guards when a prerequisite can be missing

## 3) Determinism and resilience

- Dynamic values use `generator` + `generate("...")` when possible
- Assertions are not over-specified (prefer semantic matchers)
- Cleanup uses tolerant status where API supports idempotency (`one_of([200, 204, 404])`)

## 4) Command validation

```bash
tales validate <path>
tales test <path> --seed 1234
```

Optional stress run:

```bash
tales test <path> --seed 1234 --parallel 4
```

