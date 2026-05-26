package runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/hyperxlab/tales/internal/model"
	"github.com/hyperxlab/tales/internal/provider"
	sqlprovider "github.com/hyperxlab/tales/internal/provider/sql"
	"github.com/hyperxlab/tales/internal/report"
)

func sqlConfigSuite() *model.Suite {
	return &model.Suite{
		ConfigExpr: map[string]model.Expression{
			"sql": expr(`{ connections = { app = { driver = "postgres", dsn = "postgres://u:p@host/db" } } }`),
		},
	}
}

func newSQLStep(name string, opts ...func(*model.Step)) *model.Step {
	step := &model.Step{
		Provider: "sql",
		Name:     name,
		SQL: &model.SQLCall{
			Connection: expr(`"app"`),
		},
	}

	for _, opt := range opts {
		opt(step)
	}

	return step
}

func sqlExec(statement string, args string) func(*model.Step) {
	return func(step *model.Step) {
		op := &model.SQLOp{SQL: expr(`"` + statement + `"`)}
		if args != "" {
			op.Args = expr(args)
		}

		step.SQL.Exec = op
	}
}

func sqlQuery(statement string, args string) func(*model.Step) {
	return func(step *model.Step) {
		op := &model.SQLOp{SQL: expr(`"` + statement + `"`)}
		if args != "" {
			op.Args = expr(args)
		}

		step.SQL.Query = op
	}
}

func withExpect(expect *model.Expect) func(*model.Step) {
	return func(step *model.Step) { step.Expect = expect }
}

func withCapture(m map[string]model.Expression) func(*model.Step) {
	return func(step *model.Step) { step.Capture = m }
}

func TestSQLStepExecPassesExpect(t *testing.T) {
	t.Parallel()

	sqlProv, mock := newRuntimeSQLProvider(t)
	mock.ExpectExec("UPDATE organizations").
		WithArgs(true, "org_123").
		WillReturnResult(sqlmock.NewResult(0, 1))

	runner := NewRunner(provider.NewRegistry(sqlProv))

	suite := sqlConfigSuite()
	suite.Scenarios = []*model.Scenario{{
		Name: "vip",
		Steps: []*model.Step{newSQLStep("make_vip",
			sqlExec("UPDATE organizations SET vip = $1 WHERE id = $2", `[true, "org_123"]`),
			withExpect(&model.Expect{JSON: expr(`{ rows_affected = 1 }`)}),
		)},
	}}

	result, err := runner.Run(context.Background(), suite, Options{Seed: 1, Parallel: 1})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	step := result.Scenarios[0].Steps[0]
	if step.Status != report.StatusPass {
		t.Fatalf("step status: want pass got %s (failure=%+v)", step.Status, step.Failure)
	}
}

func TestSQLStepQueryCaptureFeedsNextStep(t *testing.T) {
	t.Parallel()

	sqlProv, mock := newRuntimeSQLProvider(t)
	rows := sqlmock.NewRows([]string{"id", "vip"}).AddRow("org_123", true)
	mock.ExpectQuery("SELECT id, vip").
		WithArgs("org_123").
		WillReturnRows(rows)
	mock.ExpectExec("DELETE FROM").
		WithArgs(true).
		WillReturnResult(sqlmock.NewResult(0, 1))

	runner := NewRunner(provider.NewRegistry(sqlProv))

	suite := sqlConfigSuite()
	suite.Scenarios = []*model.Scenario{{
		Name: "capture",
		Steps: []*model.Step{
			newSQLStep("get_org",
				sqlQuery("SELECT id, vip FROM organizations WHERE id = $1", `["org_123"]`),
				withCapture(map[string]model.Expression{"vip": expr(`response.json.rows[0].vip`)}),
			),
			newSQLStep("cleanup",
				sqlExec("DELETE FROM tales_orgs WHERE vip = $1", `[result.get_org.vip]`),
			),
		},
	}}

	result, err := runner.Run(context.Background(), suite, Options{Seed: 1, Parallel: 1})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	for _, s := range result.Scenarios[0].Steps {
		if s.Status != report.StatusPass {
			t.Fatalf("step %s: want pass got %s (failure=%+v)", s.Name, s.Status, s.Failure)
		}
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sqlmock expectations: %v", err)
	}
}

func TestSQLStepEvalErrorPropagates(t *testing.T) {
	t.Parallel()

	sqlProv, _ := newRuntimeSQLProvider(t)
	runner := NewRunner(provider.NewRegistry(sqlProv))

	suite := sqlConfigSuite()
	suite.Scenarios = []*model.Scenario{{
		Name: "broken",
		Steps: []*model.Step{{
			Provider: "sql",
			Name:     "bad",
			SQL: &model.SQLCall{
				Connection: expr(`"app"`),
				Exec:       &model.SQLOp{SQL: expr(`123`)},
			},
		}},
	}}

	result, err := runner.Run(context.Background(), suite, Options{Seed: 1, Parallel: 1})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	step := result.Scenarios[0].Steps[0]
	if step.Status != report.StatusFail {
		t.Fatalf("want fail got %s", step.Status)
	}

	if step.Failure == nil || !strings.Contains(step.Failure.Message, "must be a string") {
		t.Fatalf("want eval failure mentioning string, got %+v", step.Failure)
	}
}

func TestSQLStepTeardownStillRunsAfterFailure(t *testing.T) {
	t.Parallel()

	sqlProv, mock := newRuntimeSQLProvider(t)
	mock.ExpectExec("DELETE FROM users").
		WillReturnError(context.DeadlineExceeded)
	mock.ExpectExec("DROP TABLE").
		WillReturnResult(sqlmock.NewResult(0, 0))

	runner := NewRunner(provider.NewRegistry(sqlProv))

	suite := sqlConfigSuite()
	suite.Scenarios = []*model.Scenario{{
		Name: "td",
		Steps: []*model.Step{
			newSQLStep("delete_user", sqlExec("DELETE FROM users WHERE id = 1", "")),
		},
		Teardown: []*model.Step{
			newSQLStep("drop_table", sqlExec("DROP TABLE IF EXISTS users", "")),
		},
	}}

	result, err := runner.Run(context.Background(), suite, Options{Seed: 1, Parallel: 1})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if got := result.Scenarios[0].Steps[0].Status; got != report.StatusFail {
		t.Fatalf("main step: want fail got %s", got)
	}

	if len(result.Scenarios[0].Teardown) != 1 {
		t.Fatalf("teardown step missing")
	}

	if got := result.Scenarios[0].Teardown[0].Status; got != report.StatusPass {
		t.Fatalf("teardown step: want pass got %s", got)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sqlmock expectations: %v", err)
	}
}

func TestSQLStepDoesNotLeakDSNInRequest(t *testing.T) {
	t.Parallel()

	sqlProv, mock := newRuntimeSQLProvider(t)
	mock.ExpectExec("SELECT").WillReturnResult(sqlmock.NewResult(0, 0))

	runner := NewRunner(provider.NewRegistry(sqlProv))

	suite := sqlConfigSuite()
	suite.Scenarios = []*model.Scenario{{
		Name: "leak",
		Steps: []*model.Step{newSQLStep("check",
			sqlExec("SELECT 1", ""),
		)},
	}}

	result, err := runner.Run(context.Background(), suite, Options{Seed: 1, Parallel: 1})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	stepReq := result.Scenarios[0].Steps[0].Request
	for k, v := range stepReq {
		s, ok := v.(string)
		if !ok {
			continue
		}

		if strings.Contains(s, "postgres://u:p@") {
			t.Fatalf("DSN leaked through Request[%q] = %q", k, s)
		}
	}
}

// newRuntimeSQLProvider wraps a sqlmock-backed *sql.DB inside the SQL
// provider so the runtime tests exercise the real Execute path without
// touching a database.
func newRuntimeSQLProvider(t *testing.T) (*sqlprovider.Provider, sqlmock.Sqlmock) {
	t.Helper()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}

	t.Cleanup(func() { _ = db.Close() })

	p := sqlprovider.New()
	p.Inject("app", db)

	return p, mock
}
