package parser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSQLSuite(t *testing.T, content string) string {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "sql.tales")

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	return dir
}

func TestLoadPathSQLStepExec(t *testing.T) {
	t.Parallel()

	dir := writeSQLSuite(t, `version = 1

scenario "sql exec" {
  step "sql" "make_vip" {
    connection = "app"
    exec {
      sql  = "UPDATE organizations SET vip = $1 WHERE id = $2"
      args = [true, "org_123"]
    }
    expect {
      json = {
        rows_affected = 1
      }
    }
  }
}
`)

	suite, diags := LoadPath(dir)
	if diags.HasErrors() {
		t.Fatalf("unexpected diagnostics: %s", diags.Error())
	}

	if len(suite.Scenarios) != 1 {
		t.Fatalf("want 1 scenario got %d", len(suite.Scenarios))
	}

	step := suite.Scenarios[0].Steps[0]
	if step.Provider != "sql" {
		t.Fatalf("provider: want sql got %q", step.Provider)
	}

	if step.SQL == nil {
		t.Fatalf("step.SQL should be set")
	}

	if step.SQL.Connection.Expr == nil {
		t.Fatalf("connection expression should be set")
	}

	if step.SQL.Exec == nil {
		t.Fatalf("exec block should be parsed")
	}

	if step.SQL.Exec.SQL.Expr == nil {
		t.Fatalf("exec.sql expression should be set")
	}

	if step.SQL.Exec.Args.Expr == nil {
		t.Fatalf("exec.args expression should be set")
	}

	if step.SQL.Query != nil {
		t.Fatalf("query block should be nil for an exec step")
	}
}

func TestLoadPathSQLStepQuery(t *testing.T) {
	t.Parallel()

	dir := writeSQLSuite(t, `version = 1

scenario "sql query" {
  step "sql" "get_org" {
    connection = "app"
    query {
      sql  = "SELECT id FROM organizations WHERE id = $1"
      args = ["org_123"]
    }
    capture {
      id = response.rows[0].id
    }
  }
}
`)

	suite, diags := LoadPath(dir)
	if diags.HasErrors() {
		t.Fatalf("unexpected diagnostics: %s", diags.Error())
	}

	step := suite.Scenarios[0].Steps[0]

	if step.SQL == nil || step.SQL.Query == nil {
		t.Fatalf("query block missing")
	}

	if step.SQL.Exec != nil {
		t.Fatalf("exec block should be nil for a query step")
	}

	if step.SQL.Query.SQL.Expr == nil {
		t.Fatalf("query.sql should be set")
	}
}

func TestLoadPathSQLStepRejectsExecAndQuery(t *testing.T) {
	t.Parallel()

	dir := writeSQLSuite(t, `version = 1

scenario "sql conflict" {
  step "sql" "broken" {
    connection = "app"
    exec  { sql = "DELETE FROM t WHERE id = $1" }
    query { sql = "SELECT * FROM t" }
  }
}
`)

	_, diags := LoadPath(dir)
	if !diags.HasErrors() {
		t.Fatalf("expected error when both exec and query are set")
	}

	if !strings.Contains(diags.Error(), "exactly one of exec or query") {
		t.Fatalf("expected conflict message, got: %s", diags.Error())
	}
}

func TestLoadPathSQLStepRejectsNoOp(t *testing.T) {
	t.Parallel()

	dir := writeSQLSuite(t, `version = 1

scenario "sql empty" {
  step "sql" "broken" {
    connection = "app"
  }
}
`)

	_, diags := LoadPath(dir)
	if !diags.HasErrors() {
		t.Fatalf("expected error when neither exec nor query is set")
	}

	if !strings.Contains(diags.Error(), "exec or query") {
		t.Fatalf("expected missing-op message, got: %s", diags.Error())
	}
}

func TestLoadPathSQLStepRequiresConnection(t *testing.T) {
	t.Parallel()

	dir := writeSQLSuite(t, `version = 1

scenario "sql no conn" {
  step "sql" "broken" {
    exec { sql = "DELETE FROM t" }
  }
}
`)

	_, diags := LoadPath(dir)
	if !diags.HasErrors() {
		t.Fatalf("expected error when connection is missing")
	}

	if !strings.Contains(diags.Error(), "connection") {
		t.Fatalf("expected connection diagnostic, got: %s", diags.Error())
	}
}

func TestLoadPathSQLStepRequiresExecSQL(t *testing.T) {
	t.Parallel()

	dir := writeSQLSuite(t, `version = 1

scenario "sql no sql" {
  step "sql" "broken" {
    connection = "app"
    exec { args = [1] }
  }
}
`)

	_, diags := LoadPath(dir)
	if !diags.HasErrors() {
		t.Fatalf("expected error when exec.sql is missing")
	}

	if !strings.Contains(diags.Error(), "exec.sql") {
		t.Fatalf("expected exec.sql diagnostic, got: %s", diags.Error())
	}
}

func TestLoadPathSQLFieldsRejectedOnNonSQLProvider(t *testing.T) {
	t.Parallel()

	dir := writeSQLSuite(t, `version = 1

scenario "wrong provider" {
  step "http" "wrong" {
    connection = "app"
    exec { sql = "DELETE FROM t" }
    request {
      method = "GET"
      url    = "http://localhost"
    }
  }
}
`)

	_, diags := LoadPath(dir)
	if !diags.HasErrors() {
		t.Fatalf("expected error when SQL fields appear on a non-sql provider")
	}

	if !strings.Contains(diags.Error(), "sql-only fields") {
		t.Fatalf("expected sql-only fields diagnostic, got: %s", diags.Error())
	}
}

func TestLoadPathSQLStepInTeardown(t *testing.T) {
	t.Parallel()

	dir := writeSQLSuite(t, `version = 1

scenario "sql teardown" {
  step "http" "ping" {
    request {
      method = "GET"
      url    = "http://localhost"
    }
  }

  teardown {
    step "sql" "cleanup" {
      connection = "app"
      exec {
        sql  = "DELETE FROM organizations WHERE id = $1"
        args = ["org_123"]
      }
    }
  }
}
`)

	suite, diags := LoadPath(dir)
	if diags.HasErrors() {
		t.Fatalf("unexpected diagnostics: %s", diags.Error())
	}

	if len(suite.Scenarios[0].Teardown) != 1 {
		t.Fatalf("want 1 teardown step got %d", len(suite.Scenarios[0].Teardown))
	}

	td := suite.Scenarios[0].Teardown[0]
	if td.Provider != "sql" || td.SQL == nil || td.SQL.Exec == nil {
		t.Fatalf("teardown sql step not parsed correctly: %+v", td)
	}
}
