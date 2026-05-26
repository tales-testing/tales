# SQL Provider

The `sql` provider is a pragmatic helper for preparing and tearing down
application state that is not exposed through the public HTTP / mobile API.
It is **not** a replacement for those providers — use it to set up the
preconditions, then verify the business behaviour through the same surface
your users see.

## When to use it

- Toggle internal flags that the API does not let you mutate (`vip = true`,
  feature gates, throttling counters, …).
- Reset or seed a small number of rows for a specific scenario.
- Inspect server state inside a teardown to make a clean cleanup decision.

## When not to use it

- Asserting that the application's user-facing behaviour works. Always pair
  a SQL setup with an HTTP / UI assertion that observes the visible effect.
- Bulk fixtures, migrations, schema management, snapshots. The provider has
  no opinions on any of these; use a real migration tool.
- Reading user-facing data through SQL "to be fast". The whole point of
  Tales is that the test passes through the same API a user does.

## Drivers

Tales V1 embeds two drivers:

| Driver alias            | Underlying library                | Placeholders |
|-------------------------|-----------------------------------|--------------|
| `postgres` (or `pgx`)   | `github.com/jackc/pgx/v5/stdlib`  | `$1`, `$2`   |
| `mysql`                 | `github.com/go-sql-driver/mysql`  | `?`          |

Other drivers are rejected at runtime with `unsupported sql driver "<name>"`.
Each step author writes the placeholder syntax their driver expects — Tales
does not rewrite SQL between dialects.

## Connection config

Connections live under `config.sql.connections.<name>`. They are opened
lazily on first use and closed when the suite ends.

```hcl
config {
  sql = {
    connections = {
      app = {
        driver = "postgres"
        dsn    = env("DATABASE_URL")
      }
    }
  }
}
```

`env("DATABASE_URL", "")` is the recommended pattern so the same suite can
run locally (with a docker container) and in CI (with a managed instance).
Pair it with `skip_unless { env_set = ["DATABASE_URL"] }` if the SQL
preconditions are optional.

The DSN is **never** copied into reports. Only the connection name, driver
alias, SQL text and arg count appear in the JSONL / HTML / JUnit outputs.

## Exec step

```hcl
step "sql" "make_org_vip" {
  connection = "app"

  exec {
    sql  = "UPDATE organizations SET vip = $1 WHERE id = $2"
    args = [true, result.create_org.id]
  }

  expect {
    json = {
      rows_affected = 1
    }
  }
}
```

Exec response shape:

```json
{
  "rows_affected":  1,
  "last_insert_id": null
}
```

- `rows_affected` is `null` when the driver does not support it (so suites
  using `one_of` or `optional(any())` still pass).
- `last_insert_id` is `null` on PostgreSQL by design — use a `RETURNING`
  clause + a `query` step if you need the new id.

## Query step

```hcl
step "sql" "get_org" {
  connection = "app"

  query {
    sql  = "SELECT id, vip FROM organizations WHERE id = $1"
    args = [result.create_org.id]
  }

  expect {
    json = {
      row_count = 1
      rows = [
        {
          id  = result.create_org.id
          vip = true
        },
      ]
    }
  }

  capture {
    vip = response.json.rows[0].vip
  }
}
```

Query response shape:

```json
{
  "row_count": 1,
  "columns":   ["id", "vip"],
  "rows": [
    { "id": "org_123", "vip": true }
  ]
}
```

- `rows` is a list of objects keyed by column name. Always alias your
  columns when joining two tables that share a name — duplicate column
  names fail the step with `duplicate SQL column "id"; use aliases in
  query`.
- `[]byte` columns are decoded as UTF-8 strings; non-UTF-8 bytes cause an
  explicit error rather than silent corruption.
- `time.Time` columns are returned as RFC3339Nano strings.
- PostgreSQL `jsonb` columns come back as strings. Use HCL's
  `jsondecode(response.json.rows[0].metadata).field` to descend into them.

## Teardown

The SQL provider is teardown-friendly. Combine it with the standard
`when = can(...)` pattern so the cleanup only runs when the prerequisite
captured value exists:

```hcl
teardown {
  step "sql" "reset_org_vip" {
    when       = can(result.create_org.id)
    connection = "app"

    exec {
      sql  = "UPDATE organizations SET vip = $1 WHERE id = $2"
      args = [false, result.create_org.id]
    }

    expect {
      json = {
        rows_affected = one_of([0, 1])
      }
    }
  }
}
```

## Captures

`response.json.<path>` works exactly as it does for HTTP steps. Frequent
patterns:

```hcl
capture {
  rows_affected = response.json.rows_affected
  vip           = response.json.rows[0].vip
  first_id      = response.json.rows[0].id
}
```

The captured values are exposed to downstream steps under
`result.<step_name>.<key>`.

## Args, types and conversion

Args are bound through `database/sql` as positional parameters; Tales never
interpolates them into the SQL text.

| HCL value      | Bound as           |
|----------------|--------------------|
| `null`         | SQL NULL           |
| `true / false` | `bool`             |
| integer        | `int64` (preserves `bigint` precision) |
| non-integer    | `float64`          |
| string         | `string`           |

Lists, tuples and objects are rejected explicitly:
`unsupported SQL arg type at args[<i>]: <type>`. Wrap them in a string
representation (JSON, comma-separated, …) before binding if you need to
pass a composite value.

## Security and masking

- DSNs are masked through a centralised `MaskDSN` helper. They never appear
  in reports — only the connection name, driver alias, SQL text and the
  *count* of args do.
- Error wrappers replace any embedded DSN substring with `***`.
- Args are reported in `Output.Request.args` exactly as written. If a step
  binds a secret, mark the value as such on the call site (e.g. capture it
  from a previous step's response so it is never inlined in the .tales).

## Reports

SQL steps appear with `provider: "sql"` in the JSONL, JUnit and HTML
reports. The request structure carries `connection`, `mode` (`exec` /
`query`), `sql` and `args`; the response carries the `json` payload
described above.

## Limitations (V1)

- Two drivers only: `postgres` (or alias `pgx`) and `mysql`. SQLite was
  considered and dropped to keep the cross-platform binary lean.
- No automatic placeholder rewriting between dialects. Write `$1` for
  Postgres and `?` for MySQL.
- No transactions span multiple steps. Each step uses the shared `*sql.DB`
  pool.
- Only scalar args (string / number / bool / null). Lists and objects are
  rejected at the runtime boundary.
- `last_insert_id` is `null` on PostgreSQL — use `RETURNING` + query.
- JSON columns are returned as strings; use `jsondecode(...)` if you need
  to walk them.
