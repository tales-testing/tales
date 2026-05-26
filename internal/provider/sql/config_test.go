package sql

import (
	"strings"
	"testing"

	"github.com/zclconf/go-cty/cty"
)

func buildConfig(name, driver, dsn string) map[string]cty.Value {
	connAttrs := map[string]cty.Value{}

	if driver != "" {
		connAttrs["driver"] = cty.StringVal(driver)
	}

	if dsn != "" {
		connAttrs["dsn"] = cty.StringVal(dsn)
	}

	connections := map[string]cty.Value{}
	if name != "" {
		if len(connAttrs) == 0 {
			connections[name] = cty.EmptyObjectVal
		} else {
			connections[name] = cty.ObjectVal(connAttrs)
		}
	}

	sqlAttrs := map[string]cty.Value{
		"connections": cty.EmptyObjectVal,
	}

	if len(connections) > 0 {
		sqlAttrs["connections"] = cty.ObjectVal(connections)
	}

	return map[string]cty.Value{
		"sql": cty.ObjectVal(sqlAttrs),
	}
}

func TestResolveConnectionOK(t *testing.T) {
	t.Parallel()

	conf := buildConfig("app", "postgres", "postgres://x:y@host/db")

	got, err := resolveConnection(conf, "app")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.Driver != "postgres" {
		t.Errorf("driver: want postgres got %q", got.Driver)
	}

	if got.DSN != "postgres://x:y@host/db" {
		t.Errorf("dsn: want postgres://x:y@host/db got %q", got.DSN)
	}
}

func TestResolveConnectionMissing(t *testing.T) {
	t.Parallel()

	_, err := resolveConnection(buildConfig("app", "postgres", "postgres://x:y@host/db"), "other")
	if err == nil || !strings.Contains(err.Error(), `connection "other" not found`) {
		t.Fatalf("expected not-found error, got %v", err)
	}
}

func TestResolveConnectionEmptyDriver(t *testing.T) {
	t.Parallel()

	_, err := resolveConnection(buildConfig("app", "", "postgres://x:y@host/db"), "app")
	if err == nil || !strings.Contains(err.Error(), "empty driver") {
		t.Fatalf("expected empty-driver error, got %v", err)
	}
}

func TestResolveConnectionEmptyDSN(t *testing.T) {
	t.Parallel()

	_, err := resolveConnection(buildConfig("app", "postgres", ""), "app")
	if err == nil || !strings.Contains(err.Error(), "empty dsn") {
		t.Fatalf("expected empty-dsn error, got %v", err)
	}
}

func TestResolveConnectionMissingSQLBlock(t *testing.T) {
	t.Parallel()

	_, err := resolveConnection(map[string]cty.Value{}, "app")
	if err == nil || !strings.Contains(err.Error(), "config.sql is not defined") {
		t.Fatalf("expected missing-config error, got %v", err)
	}
}

func TestResolveDriverAliases(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"postgres": "pgx",
		"pgx":      "pgx",
		"mysql":    "mysql",
	}

	for alias, want := range cases {
		got, err := resolveDriver(alias)
		if err != nil {
			t.Errorf("resolveDriver(%q): unexpected error %v", alias, err)

			continue
		}

		if got != want {
			t.Errorf("resolveDriver(%q): want %q got %q", alias, want, got)
		}
	}

	if _, err := resolveDriver("oracle"); err == nil {
		t.Errorf("resolveDriver(oracle): want error, got nil")
	}
}
