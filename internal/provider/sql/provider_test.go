package sql

import (
	"context"
	dbsql "database/sql"
	"strings"
	"sync"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/hyperxlab/tales/internal/provider"
	"github.com/zclconf/go-cty/cty"
)

func mockConfig() map[string]cty.Value {
	return map[string]cty.Value{
		"sql": cty.ObjectVal(map[string]cty.Value{
			"connections": cty.ObjectVal(map[string]cty.Value{
				"app": cty.ObjectVal(map[string]cty.Value{
					"driver": cty.StringVal("postgres"),
					"dsn":    cty.StringVal("postgres://user:secret@localhost/db"),
				}),
			}),
		}),
	}
}

func newProviderWithMock(t *testing.T) (*Provider, *dbsql.DB, sqlmock.Sqlmock) {
	t.Helper()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New failed: %v", err)
	}

	p := New()
	p.Inject("app", db)

	return p, db, mock
}

func TestProviderExecutesExec(t *testing.T) {
	t.Parallel()

	p, db, mock := newProviderWithMock(t)
	defer func() { _ = db.Close() }()

	mock.ExpectExec("UPDATE organizations").
		WithArgs(true, "org_123").
		WillReturnResult(sqlmock.NewResult(0, 1))

	out, err := p.Execute(context.Background(), provider.Input{
		Config: mockConfig(),
		SQL: &provider.SQLExecution{
			Connection: "app",
			Mode:       "exec",
			SQL:        "UPDATE organizations SET vip = $1 WHERE id = $2",
			Args:       []any{true, "org_123"},
		},
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	rowsAffected := out.Response["rows_affected"]
	if !rowsAffected.RawEquals(cty.NumberIntVal(1)) {
		t.Errorf("rows_affected: want 1 got %#v", rowsAffected)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("sqlmock expectations: %v", err)
	}
}

func TestProviderExecutesQuery(t *testing.T) {
	t.Parallel()

	p, db, mock := newProviderWithMock(t)
	defer func() { _ = db.Close() }()

	rows := sqlmock.NewRows([]string{"id", "vip"}).
		AddRow("org_123", true)

	mock.ExpectQuery("SELECT id, vip FROM organizations").
		WithArgs("org_123").
		WillReturnRows(rows)

	out, err := p.Execute(context.Background(), provider.Input{
		Config: mockConfig(),
		SQL: &provider.SQLExecution{
			Connection: "app",
			Mode:       "query",
			SQL:        "SELECT id, vip FROM organizations WHERE id = $1",
			Args:       []any{"org_123"},
		},
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	rowCount := out.Response["row_count"]
	if !rowCount.RawEquals(cty.NumberIntVal(1)) {
		t.Errorf("row_count: want 1 got %#v", rowCount)
	}

	rowsValue := out.Response["rows"]
	if rowsValue.Type().FriendlyName() == "tuple of 0 elements" {
		t.Fatal("rows tuple should not be empty")
	}

	firstRow := rowsValue.Index(cty.NumberIntVal(0))
	if !firstRow.GetAttr("id").RawEquals(cty.StringVal("org_123")) {
		t.Errorf("row.id: want org_123 got %#v", firstRow.GetAttr("id"))
	}

	if !firstRow.GetAttr("vip").RawEquals(cty.True) {
		t.Errorf("row.vip: want true got %#v", firstRow.GetAttr("vip"))
	}
}

func TestProviderQueryRejectsDuplicateColumns(t *testing.T) {
	t.Parallel()

	p, db, mock := newProviderWithMock(t)
	defer func() { _ = db.Close() }()

	rows := sqlmock.NewRows([]string{"id", "id"}).AddRow(1, 2)
	mock.ExpectQuery("SELECT").WillReturnRows(rows)

	_, err := p.Execute(context.Background(), provider.Input{
		Config: mockConfig(),
		SQL: &provider.SQLExecution{
			Connection: "app",
			Mode:       "query",
			SQL:        "SELECT a.id, b.id FROM a JOIN b ON a.x = b.x",
		},
	})
	if err == nil || !strings.Contains(err.Error(), "duplicate SQL column") {
		t.Fatalf("expected duplicate column error, got %v", err)
	}
}

func TestProviderExecRowsAffectedNullOnDriverError(t *testing.T) {
	t.Parallel()

	p, db, mock := newProviderWithMock(t)
	defer func() { _ = db.Close() }()

	mock.ExpectExec("DELETE FROM t").
		WillReturnResult(sqlmock.NewErrorResult(dbsql.ErrConnDone))

	out, err := p.Execute(context.Background(), provider.Input{
		Config: mockConfig(),
		SQL: &provider.SQLExecution{
			Connection: "app",
			Mode:       "exec",
			SQL:        "DELETE FROM t",
		},
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !out.Response["rows_affected"].IsNull() {
		t.Errorf("rows_affected: want null got %#v", out.Response["rows_affected"])
	}

	if !out.Response["last_insert_id"].IsNull() {
		t.Errorf("last_insert_id: want null got %#v", out.Response["last_insert_id"])
	}
}

func TestProviderUnknownMode(t *testing.T) {
	t.Parallel()

	p, db, _ := newProviderWithMock(t)
	defer func() { _ = db.Close() }()

	_, err := p.Execute(context.Background(), provider.Input{
		Config: mockConfig(),
		SQL: &provider.SQLExecution{
			Connection: "app",
			Mode:       "lookup",
			SQL:        "SELECT 1",
		},
	})
	if err == nil || !strings.Contains(err.Error(), "unknown mode") {
		t.Fatalf("expected unknown-mode error, got %v", err)
	}
}

func TestProviderMissingExecution(t *testing.T) {
	t.Parallel()

	p := New()

	if _, err := p.Execute(context.Background(), provider.Input{Config: mockConfig()}); err == nil {
		t.Fatalf("expected error when SQLExecution is nil")
	}
}

func TestProviderErrorOmitsArgValues(t *testing.T) {
	t.Parallel()

	p, db, mock := newProviderWithMock(t)
	defer func() { _ = db.Close() }()

	mock.ExpectExec("UPDATE secret_table").
		WithArgs("super-secret-token", "user_42").
		WillReturnError(dbsql.ErrConnDone)

	_, err := p.Execute(context.Background(), provider.Input{
		Config: mockConfig(),
		SQL: &provider.SQLExecution{
			Connection: "app",
			Mode:       "exec",
			SQL:        "UPDATE secret_table SET tok = $1 WHERE id = $2",
			Args:       []any{"super-secret-token", "user_42"},
		},
	})
	if err == nil {
		t.Fatalf("expected exec error")
	}

	if strings.Contains(err.Error(), "super-secret-token") {
		t.Errorf("error must not include raw arg values: %s", err.Error())
	}

	if !strings.Contains(err.Error(), "args: 2 value(s) omitted") {
		t.Errorf("error must mention omitted args count, got: %s", err.Error())
	}
}

func TestProviderConnectionCacheIsReused(t *testing.T) {
	t.Parallel()

	p, db, mock := newProviderWithMock(t)
	defer func() { _ = db.Close() }()

	mock.ExpectExec("INSERT").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT").WillReturnResult(sqlmock.NewResult(0, 1))

	exec := &provider.SQLExecution{
		Connection: "app",
		Mode:       "exec",
		SQL:        "INSERT INTO t (x) VALUES ($1)",
		Args:       []any{1},
	}

	var wg sync.WaitGroup

	wg.Add(2)

	for range 2 {
		go func() {
			defer wg.Done()

			if _, err := p.Execute(context.Background(), provider.Input{Config: mockConfig(), SQL: exec}); err != nil {
				t.Errorf("execute: %v", err)
			}
		}()
	}

	wg.Wait()

	if got := len(p.conns); got != 1 {
		t.Errorf("connection cache size: want 1 got %d", got)
	}
}

func TestProviderCloseEmptiesCache(t *testing.T) {
	t.Parallel()

	p := New()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock: %v", err)
	}

	mock.ExpectClose()

	p.Inject("app", db)

	if err := p.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	if len(p.conns) != 0 {
		t.Errorf("conns: want empty after close got %d", len(p.conns))
	}
}
