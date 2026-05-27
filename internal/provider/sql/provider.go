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

	db, err := p.acquire(ctx, conn)
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

// pingTimeout bounds the initial PingContext that validates connectivity at
// the first acquire. It is intentionally aligned with the dial-timeout
// default from dsn.go: a connectivity check that takes longer than the dial
// budget would be inconsistent with the user-facing promise.
const pingTimeout = 10 * time.Second

// acquire returns the cached *sql.DB for a connection, opening it on first
// use. sql.Open is intentionally called outside the lock — it is cheap and
// non-blocking, but doing the actual handshake here would serialize every
// scenario start.
//
// Before caching, the new handle is validated with a bounded PingContext.
// A failed ping closes the handle and surfaces a clear error instead of
// caching a broken pool — the next call will dial again with the same
// bounded budget rather than reusing a poisoned connection.
func (p *Provider) acquire(ctx context.Context, conn ConnectionConfig) (*dbsql.DB, error) {
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

	// Bound the initial dial so a missing database fails fast instead of
	// stalling the runner silently (see dsn.go for the failure mode).
	effectiveDSN := injectDefaultDialTimeout(conn.Driver, conn.DSN)

	opened, err := dbsql.Open(driverName, effectiveDSN)
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}

	opened.SetMaxOpenConns(10)
	opened.SetMaxIdleConns(5)
	opened.SetConnMaxLifetime(5 * time.Minute)

	if err := pingWithBoundedTimeout(ctx, opened); err != nil {
		_ = opened.Close()

		return nil, fmt.Errorf("ping: %w", err)
	}

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

// pingWithBoundedTimeout validates a freshly opened *sql.DB with a deadline
// that is at most pingTimeout, but never longer than the parent ctx's own
// deadline if one is set. This way --timeout and per-step timeouts always
// win over the default — they can only make the check stricter.
func pingWithBoundedTimeout(parent context.Context, db *dbsql.DB) error {
	pingCtx, cancel := context.WithTimeout(parent, pingTimeout)
	defer cancel()

	budget := effectivePingBudget(pingCtx)

	if err := db.PingContext(pingCtx); err != nil {
		return fmt.Errorf("database did not respond within %s: %w", budget, err)
	}

	return nil
}

// effectivePingBudget reports the budget that actually applies to the ping —
// either the full pingTimeout, or the smaller window remaining on the parent
// ctx when it carries a stricter deadline. Used in the error message so a
// user who set --timeout=2s sees "did not respond within 2s" instead of a
// misleading "within 10s". Computed before PingContext so the value reflects
// the budget the call started with, not how much was left after it failed.
func effectivePingBudget(pingCtx context.Context) time.Duration {
	deadline, ok := pingCtx.Deadline()
	if !ok {
		return pingTimeout
	}

	budget := time.Until(deadline)
	if budget <= 0 {
		// Parent was already cancelled before we got here: the ctx has
		// no real budget to report. Fall back to the nominal cap so
		// the message stays meaningful ("within 10s") rather than "0s".
		return pingTimeout
	}

	// Round to the second so the message stays human-readable ("2s")
	// instead of leaking sub-second noise from the clock read.
	rounded := budget.Round(time.Second)
	if rounded == 0 {
		rounded = time.Second
	}

	if rounded > pingTimeout {
		return pingTimeout
	}

	return rounded
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
