package sql

import (
	"context"
	dbsql "database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/hyperxlab/tales/internal/provider"
	"github.com/zclconf/go-cty/cty"
)

const providerTypeSQL = "sql"

// Provider executes step "sql" via database/sql. It owns a per-process cache
// of *sql.DB instances, opened lazily on first use and closed when the
// runner finishes the suite.
type Provider struct {
	mu    sync.Mutex
	conns map[string]*dbsql.DB
}

// New creates an empty SQL provider. Drivers are registered by the package
// imports.go file (pgx + mysql) at startup.
func New() *Provider {
	return &Provider{conns: map[string]*dbsql.DB{}}
}

// Type returns the provider type label "sql".
func (p *Provider) Type() string {
	return providerTypeSQL
}

// Close releases every cached *sql.DB. The runner calls it once at the end
// of a suite via the io.Closer type assertion path.
func (p *Provider) Close() error {
	p.mu.Lock()

	conns := p.conns
	p.conns = map[string]*dbsql.DB{}

	p.mu.Unlock()

	var firstErr error

	for _, db := range conns {
		if err := db.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	if firstErr != nil {
		return fmt.Errorf("close sql connections: %w", firstErr)
	}

	return nil
}

// Execute runs one SQL exec or query as described by input.SQL.
func (p *Provider) Execute(ctx context.Context, input provider.Input) (*provider.Output, error) {
	if input.SQL == nil {
		return nil, fmt.Errorf("sql step is missing execution data")
	}

	conn, err := resolveConnection(input.Config, input.SQL.Connection)
	if err != nil {
		return nil, err
	}

	db, err := p.acquire(conn)
	if err != nil {
		return nil, fmt.Errorf("open sql connection %q: %w", conn.Name, withMaskedDSN(err, conn.DSN))
	}

	stepCtx, cancel := withTimeout(ctx, input.Timeout)
	defer cancel()

	start := time.Now()
	requestMap := buildRequestMap(input.SQL)

	switch input.SQL.Mode {
	case "exec":
		return p.executeExec(stepCtx, db, input.SQL, conn, requestMap, start)
	case "query":
		return p.executeQuery(stepCtx, db, input.SQL, conn, requestMap, start)
	default:
		return nil, fmt.Errorf("sql step has unknown mode %q", input.SQL.Mode)
	}
}

func (p *Provider) executeExec(ctx context.Context, db *dbsql.DB, exec *provider.SQLExecution, conn ConnectionConfig, request map[string]cty.Value, start time.Time) (*provider.Output, error) {
	result, err := db.ExecContext(ctx, exec.SQL, exec.Args...)
	if err != nil {
		return nil, sqlExecutionError("exec", conn, exec, err)
	}

	return &provider.Output{
		Request:  request,
		Response: toExecResponse(result),
		Duration: time.Since(start),
	}, nil
}

func (p *Provider) executeQuery(ctx context.Context, db *dbsql.DB, exec *provider.SQLExecution, conn ConnectionConfig, request map[string]cty.Value, start time.Time) (*provider.Output, error) {
	rows, columns, err := scanRows(ctx, db, exec.SQL, exec.Args)
	if err != nil {
		return nil, sqlExecutionError("query", conn, exec, err)
	}

	return &provider.Output{
		Request:  request,
		Response: toQueryResponse(rows, columns),
		Duration: time.Since(start),
	}, nil
}

// acquire returns the cached *sql.DB for a connection, opening it on first
// use. sql.Open is intentionally called outside the lock — it is cheap and
// non-blocking, but doing the actual handshake here would serialize every
// scenario start.
func (p *Provider) acquire(conn ConnectionConfig) (*dbsql.DB, error) {
	p.mu.Lock()

	db, ok := p.conns[conn.Name]
	if ok {
		p.mu.Unlock()

		return db, nil
	}

	p.mu.Unlock()

	driverName, err := resolveDriver(conn.Driver)
	if err != nil {
		return nil, err
	}

	opened, err := dbsql.Open(driverName, conn.DSN)
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}

	opened.SetMaxOpenConns(10)
	opened.SetMaxIdleConns(5)
	opened.SetConnMaxLifetime(5 * time.Minute)

	p.mu.Lock()
	defer p.mu.Unlock()

	if existing, ok := p.conns[conn.Name]; ok {
		// Another goroutine raced us; discard our new handle.
		_ = opened.Close()

		return existing, nil
	}

	p.conns[conn.Name] = opened

	return opened, nil
}

// withTimeout wraps the parent ctx in a deadline when timeout > 0. Returning
// a no-op cancel for the zero case lets callers always defer the cancel.
func withTimeout(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return parent, func() {}
	}

	return context.WithTimeout(parent, timeout)
}

// Inject lets tests bypass driver registration and inject a pre-built
// *sql.DB. It is exported only for the package's own tests and for runtime
// wiring; .tales scenarios never reach it directly.
func (p *Provider) Inject(name string, db *dbsql.DB) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.conns[name] = db
}

func buildRequestMap(execution *provider.SQLExecution) map[string]cty.Value {
	args := make([]cty.Value, 0, len(execution.Args))

	for _, raw := range execution.Args {
		value, err := ConvertRowValue(raw)
		if err != nil {
			args = append(args, cty.StringVal(fmt.Sprintf("%v", raw)))

			continue
		}

		args = append(args, value)
	}

	argsValue := cty.EmptyTupleVal
	if len(args) > 0 {
		argsValue = cty.TupleVal(args)
	}

	return map[string]cty.Value{
		"connection": cty.StringVal(execution.Connection),
		"mode":       cty.StringVal(execution.Mode),
		"sql":        cty.StringVal(execution.SQL),
		"args":       argsValue,
	}
}

// sqlExecutionError wraps a driver error with sanitized context. The DSN is
// never propagated as-is; only the connection name and driver alias are
// surfaced.
func sqlExecutionError(mode string, conn ConnectionConfig, exec *provider.SQLExecution, err error) error {
	return fmt.Errorf("SQL %s failed\nconnection: %s\ndriver: %s\nsql: %s\nargs: %d value(s) omitted\nerror: %w",
		mode, conn.Name, conn.Driver, exec.SQL, len(exec.Args), withMaskedDSN(err, conn.DSN))
}

// withMaskedDSN scrubs DSN credential fragments out of an error message
// while preserving the wrapped chain so callers keep using errors.Is/As.
// Some drivers embed the DSN verbatim in their error strings; we override
// Error() but still Unwrap() to the original.
func withMaskedDSN(err error, dsn string) error {
	if err == nil || dsn == "" {
		return err
	}

	masked := MaskDSN(dsn)
	if masked == dsn {
		return err
	}

	msg := strings.ReplaceAll(err.Error(), dsn, masked)
	if msg == err.Error() {
		return err
	}

	return &maskedError{msg: msg, wrapped: err}
}

// maskedError wraps an error with a sanitized message but keeps the
// original reachable through errors.Unwrap.
type maskedError struct {
	msg     string
	wrapped error
}

func (e *maskedError) Error() string { return e.msg }
func (e *maskedError) Unwrap() error { return e.wrapped }
