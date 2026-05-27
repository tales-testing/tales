package sql

import "testing"

func TestInjectDefaultDialTimeout_MySQL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "no query string appends ?timeout",
			in:   "user:pass@tcp(127.0.0.1:3306)/db",
			want: "user:pass@tcp(127.0.0.1:3306)/db?timeout=10s",
		},
		{
			name: "existing query string appends &timeout",
			in:   "user:pass@tcp(127.0.0.1:3306)/db?parseTime=true",
			want: "user:pass@tcp(127.0.0.1:3306)/db?parseTime=true&timeout=10s",
		},
		{
			name: "explicit timeout is preserved verbatim",
			in:   "user:pass@tcp(127.0.0.1:3306)/db?timeout=2s",
			want: "user:pass@tcp(127.0.0.1:3306)/db?timeout=2s",
		},
		{
			name: "case-insensitive timeout match preserves user value",
			in:   "user:pass@tcp(127.0.0.1:3306)/db?Timeout=1s",
			want: "user:pass@tcp(127.0.0.1:3306)/db?Timeout=1s",
		},
		{
			name: "readTimeout alone does NOT count as dial timeout",
			in:   "user:pass@tcp(127.0.0.1:3306)/db?readTimeout=30s",
			want: "user:pass@tcp(127.0.0.1:3306)/db?readTimeout=30s&timeout=10s",
		},
		{
			name: "writeTimeout alone does NOT count as dial timeout",
			in:   "user:pass@tcp(127.0.0.1:3306)/db?writeTimeout=30s",
			want: "user:pass@tcp(127.0.0.1:3306)/db?writeTimeout=30s&timeout=10s",
		},
		{
			name: "empty DSN passes through",
			in:   "",
			want: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := injectDefaultDialTimeout(driverAliasMySQL, tc.in)
			if got != tc.want {
				t.Errorf("injectDefaultDialTimeout(mysql, %q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestInjectDefaultDialTimeout_Postgres(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		alias string
		in    string
		want  string
	}{
		{
			name: "URL form without query appends ?connect_timeout",
			in:   "postgres://user:pass@localhost:5432/db",
			want: "postgres://user:pass@localhost:5432/db?connect_timeout=10",
		},
		{
			name: "URL form with query appends &connect_timeout",
			in:   "postgres://user:pass@localhost:5432/db?sslmode=disable",
			want: "postgres://user:pass@localhost:5432/db?sslmode=disable&connect_timeout=10",
		},
		{
			name: "URL form with explicit connect_timeout is preserved",
			in:   "postgres://user:pass@localhost:5432/db?connect_timeout=2",
			want: "postgres://user:pass@localhost:5432/db?connect_timeout=2",
		},
		{
			name: "postgresql:// scheme also matches URL form",
			in:   "postgresql://user:pass@localhost/db",
			want: "postgresql://user:pass@localhost/db?connect_timeout=10",
		},
		{
			name: "libpq key=value DSN gets a space-separated pair",
			in:   "host=localhost port=5432 user=tales dbname=db",
			want: "host=localhost port=5432 user=tales dbname=db connect_timeout=10",
		},
		{
			name: "libpq DSN with explicit connect_timeout is preserved",
			in:   "host=localhost connect_timeout=2",
			want: "host=localhost connect_timeout=2",
		},
		{
			name:  "pgx alias is treated like postgres",
			alias: driverAliasPgx,
			in:    "postgres://localhost/db",
			want:  "postgres://localhost/db?connect_timeout=10",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			alias := tc.alias
			if alias == "" {
				alias = driverAliasPostgres
			}

			got := injectDefaultDialTimeout(alias, tc.in)
			if got != tc.want {
				t.Errorf("injectDefaultDialTimeout(%s, %q) = %q, want %q", alias, tc.in, got, tc.want)
			}
		})
	}
}

func TestInjectDefaultDialTimeout_UnknownDriverIsUntouched(t *testing.T) {
	t.Parallel()

	got := injectDefaultDialTimeout("sqlite", "file:test.db")
	if got != "file:test.db" {
		t.Errorf("unknown driver alias must pass DSN through unchanged, got %q", got)
	}
}
