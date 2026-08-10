package sql

import (
	"reflect"
	"strings"
	"testing"
)

func placeholderIndexes(t *testing.T, driver, sql string) []int {
	t.Helper()

	d, err := dialectFor(driver)
	if err != nil {
		t.Fatalf("dialectFor(%q): %v", driver, err)
	}

	found, err := scanPlaceholders(d, sql)
	if err != nil {
		t.Fatalf("scanPlaceholders: %v", err)
	}

	out := make([]int, 0, len(found))
	for _, p := range found {
		out = append(out, p.index)
	}

	return out
}

// The scanner must ignore anything that is not executable code: a `?` inside a
// string or a `$2` inside a comment would otherwise shift the renumbering.
func TestScanPlaceholders(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		driver string
		sql    string
		want   []int
	}{
		{"postgres plain", "postgres", "SELECT * FROM t WHERE id = $1 AND b = $2", []int{1, 2}},
		{"postgres string literal", "postgres", "SELECT $1 WHERE s = 'a $2 b'", []int{1}},
		{"postgres doubled quote", "postgres", "SELECT 'it''s $9', $1", []int{1}},
		{"postgres quoted identifier", "postgres", `SELECT "col$2" FROM t WHERE id = $1`, []int{1}},
		{"postgres line comment", "postgres", "SELECT $1 -- $2\n, $3", []int{1, 3}},
		{"postgres nested block comment", "postgres", "SELECT $1 /* $2 /* $3 */ $4 */, $5", []int{1, 5}},
		{"postgres dollar quoted", "postgres", "SELECT $$body $1$$, $2", []int{2}},
		{"postgres tagged dollar quote", "postgres", "SELECT $tag$ $1 $tag$, $2", []int{2}},
		{"postgres escape string", "postgres", `SELECT E'a\'$1', $2`, []int{2}},
		{"postgres jsonb question operator", "postgres", "SELECT data ? 'k', $1", []int{1}},
		{"postgres cast", "postgres", "SELECT $1::text, $2", []int{1, 2}},
		{"postgres bare dollar", "postgres", "SELECT '$', $1", []int{1}},
		{"postgres out of order", "postgres", "SELECT * FROM t WHERE b = $2 AND a = $1", []int{2, 1}},

		{"mysql plain", "mysql", "SELECT * FROM t WHERE id = ? AND b = ?", []int{1, 2}},
		{"mysql string literal", "mysql", "SELECT ? WHERE s = 'a ? b'", []int{1}},
		{"mysql backtick identifier", "mysql", "SELECT `c?l` FROM t WHERE id = ?", []int{1}},
		{"mysql line comment", "mysql", "SELECT ? -- ?\n, ?", []int{1, 2}},
		{"mysql dash without space is not a comment", "mysql", "SELECT ? WHERE a=1--? AND b=?", []int{1, 2, 3}},
		{"mysql hash comment", "mysql", "SELECT ? # ?\n, ?", []int{1, 2}},
		{"mysql double quoted", "mysql", `SELECT "a?b", ?`, []int{1}},
		{"mysql backslash escape", "mysql", `SELECT 'a\'?', ?`, []int{1}},
		{"mysql block comment", "mysql", "SELECT ? /* ? */, ?", []int{1, 2}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := placeholderIndexes(t, tc.driver, tc.sql)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("placeholders = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestScanPlaceholdersRejectsUnknownDriver(t *testing.T) {
	t.Parallel()

	if _, err := dialectFor("sqlite"); err == nil {
		t.Fatal("dialectFor(sqlite): want an error")
	}
}

func TestExpandListArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		driver   string
		sql      string
		args     []any
		wantSQL  string
		wantArgs []any
		wantErr  string
	}{
		{
			name:     "postgres simple list",
			driver:   "postgres",
			sql:      "SELECT * FROM t WHERE id IN ($1)",
			args:     []any{ListArg{int64(1), int64(2), int64(3)}},
			wantSQL:  "SELECT * FROM t WHERE id IN ($1,$2,$3)",
			wantArgs: []any{int64(1), int64(2), int64(3)},
		},
		{
			name:     "postgres renumbers later placeholders",
			driver:   "postgres",
			sql:      "SELECT * FROM t WHERE a = $1 AND id IN ($2) AND c = $3",
			args:     []any{"x", ListArg{int64(1), int64(2), int64(3)}, "y"},
			wantSQL:  "SELECT * FROM t WHERE a = $1 AND id IN ($2,$3,$4) AND c = $5",
			wantArgs: []any{"x", int64(1), int64(2), int64(3), "y"},
		},
		{
			name:     "postgres out of textual order",
			driver:   "postgres",
			sql:      "SELECT * FROM t WHERE b = $2 AND a IN ($1)",
			args:     []any{ListArg{int64(7), int64(8)}, "z"},
			wantSQL:  "SELECT * FROM t WHERE b = $3 AND a IN ($1,$2)",
			wantArgs: []any{int64(7), int64(8), "z"},
		},
		{
			name:     "postgres reused scalar keeps one base",
			driver:   "postgres",
			sql:      "SELECT * FROM t WHERE a = $1 OR b = $1 AND id IN ($2)",
			args:     []any{"x", ListArg{int64(1), int64(2)}},
			wantSQL:  "SELECT * FROM t WHERE a = $1 OR b = $1 AND id IN ($2,$3)",
			wantArgs: []any{"x", int64(1), int64(2)},
		},
		{
			name:     "postgres several lists",
			driver:   "postgres",
			sql:      "SELECT * FROM t WHERE a IN ($1) AND b IN ($2)",
			args:     []any{ListArg{int64(1), int64(2)}, ListArg{"x", "y", "z"}},
			wantSQL:  "SELECT * FROM t WHERE a IN ($1,$2) AND b IN ($3,$4,$5)",
			wantArgs: []any{int64(1), int64(2), "x", "y", "z"},
		},
		{
			name:    "postgres list used twice",
			driver:  "postgres",
			sql:     "SELECT * FROM t WHERE a IN ($1) OR b IN ($1)",
			args:    []any{ListArg{int64(1)}},
			wantErr: "may appear only once",
		},
		{
			name:    "postgres list never referenced",
			driver:  "postgres",
			sql:     "SELECT * FROM t WHERE a = $2",
			args:    []any{ListArg{int64(1)}, "x"},
			wantErr: "does not appear in the statement",
		},
		{
			name:    "postgres placeholder out of range",
			driver:  "postgres",
			sql:     "SELECT * FROM t WHERE a IN ($1) AND b = $5",
			args:    []any{ListArg{int64(1)}, "x"},
			wantErr: "references $5 but only 2 argument(s)",
		},
		{
			name:    "postgres empty list",
			driver:  "postgres",
			sql:     "SELECT * FROM t WHERE a IN ($1)",
			args:    []any{ListArg{}},
			wantErr: "empty list",
		},
		{
			name:    "postgres list only inside a dollar quote",
			driver:  "postgres",
			sql:     "SELECT $$ $1 $$",
			args:    []any{ListArg{int64(1)}},
			wantErr: "does not appear in the statement",
		},
		{
			name:     "mysql simple list",
			driver:   "mysql",
			sql:      "SELECT * FROM t WHERE id IN (?)",
			args:     []any{ListArg{int64(1), int64(2), int64(3)}},
			wantSQL:  "SELECT * FROM t WHERE id IN (?,?,?)",
			wantArgs: []any{int64(1), int64(2), int64(3)},
		},
		{
			name:     "mysql mixes scalars and lists",
			driver:   "mysql",
			sql:      "SELECT * FROM t WHERE a = ? AND id IN (?) AND c = ?",
			args:     []any{"x", ListArg{int64(1), int64(2)}, "y"},
			wantSQL:  "SELECT * FROM t WHERE a = ? AND id IN (?,?) AND c = ?",
			wantArgs: []any{"x", int64(1), int64(2), "y"},
		},
		{
			name:    "mysql placeholder count mismatch",
			driver:  "mysql",
			sql:     "SELECT * FROM t WHERE id IN (?) AND a = ?",
			args:    []any{ListArg{int64(1)}},
			wantErr: "2 placeholder(s) but 1 argument(s)",
		},
		{
			name:    "mysql empty list",
			driver:  "mysql",
			sql:     "SELECT * FROM t WHERE id IN (?)",
			args:    []any{ListArg{}},
			wantErr: "empty list",
		},
		{
			name:    "unknown driver",
			driver:  "sqlite",
			sql:     "SELECT * FROM t WHERE id IN (?)",
			args:    []any{ListArg{int64(1)}},
			wantErr: "unsupported sql driver",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			gotSQL, gotArgs, err := expandListArgs(tc.driver, tc.sql, tc.args)

			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want it to contain %q", err, tc.wantErr)
				}

				return
			}

			if err != nil {
				t.Fatalf("expandListArgs: %v", err)
			}

			if gotSQL != tc.wantSQL {
				t.Fatalf("sql = %q, want %q", gotSQL, tc.wantSQL)
			}

			if !reflect.DeepEqual(gotArgs, tc.wantArgs) {
				t.Fatalf("args = %#v, want %#v", gotArgs, tc.wantArgs)
			}
		})
	}
}

// Statements binding only scalars must not be touched at all, so the existing
// behavior cannot regress and the common path pays nothing.
func TestExpandListArgsIsNoOpWithoutLists(t *testing.T) {
	t.Parallel()

	sql := "SELECT * FROM t WHERE id = $1 AND b = $9"
	args := []any{"x", "y"}

	gotSQL, gotArgs, err := expandListArgs("postgres", sql, args)
	if err != nil {
		t.Fatalf("expandListArgs: %v", err)
	}

	if gotSQL != sql {
		t.Fatalf("sql = %q, want it unchanged", gotSQL)
	}

	if &gotArgs[0] != &args[0] {
		t.Fatal("args slice was copied; the scalar path must return it as-is")
	}
}

// An unreachable database must not be a prerequisite for reporting a malformed
// list arg: expansion happens before the connection is acquired.
func TestExpandListArgsRejectsBeforeAnyIO(t *testing.T) {
	t.Parallel()

	_, _, err := expandListArgs("postgres", "SELECT 1", []any{ListArg{int64(1)}})
	if err == nil {
		t.Fatal("want an error for a list arg with no placeholder")
	}
}
