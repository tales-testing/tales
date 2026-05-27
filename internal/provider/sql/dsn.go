package sql

import (
	"regexp"
	"strings"
)

// The injected default of 10s for the initial TCP+handshake bounds the
// failure mode where a missing or unreachable database on a Linux runner
// (e.g. localhost:33068 with SYN drops) would let db.ExecContext block for
// ~127s through the OS TCP retry stack — silently, because runner.Run does
// not print anything until it returns. Ten seconds is short enough to fail
// fast in CI yet long enough to absorb a normally slow container boot.

// mysqlTimeoutParamRegex matches the dial-timeout DSN parameter in the MySQL
// go-sql-driver format. Anchored to avoid matching readTimeout/writeTimeout —
// only the "timeout" key controls dial.
var mysqlTimeoutParamRegex = regexp.MustCompile(`(?i)(^|[?&;])timeout=`)

// postgresConnectTimeoutRegex matches the dial-timeout parameter in both
// Postgres DSN styles: URL ("?connect_timeout=…") and libpq key=value
// ("connect_timeout=…").
var postgresConnectTimeoutRegex = regexp.MustCompile(`(?i)(^|[\s?&])connect_timeout=`)

// injectDefaultDialTimeout returns a DSN augmented with a sane default dial
// timeout when the user has not specified one. The original DSN is preserved
// when it already carries a timeout, so callers cannot accidentally widen a
// stricter user-set bound. Unknown drivers fall through untouched.
//
// For MySQL the injected parameter is "timeout=10s"; for Postgres it is
// "connect_timeout=10" (seconds, libpq convention). The read/write per-query
// timeout is intentionally NOT touched: it is bounded by ctx via --timeout or
// per-step request.timeout, and a too-aggressive default would cut legitimate
// long-running migrations.
func injectDefaultDialTimeout(driverAlias, dsn string) string {
	switch driverAlias {
	case driverAliasMySQL:
		return injectMySQLDialTimeout(dsn)
	case driverAliasPostgres, driverAliasPgx:
		return injectPostgresConnectTimeout(dsn)
	default:
		return dsn
	}
}

func injectMySQLDialTimeout(dsn string) string {
	if dsn == "" {
		return dsn
	}

	if mysqlTimeoutParamRegex.MatchString(dsn) {
		return dsn
	}

	param := "timeout=10s"

	// MySQL DSNs may or may not already carry a query string. The driver
	// uses "?" as the separator between the dbname and parameters.
	if strings.Contains(dsn, "?") {
		return dsn + "&" + param
	}

	return dsn + "?" + param
}

func injectPostgresConnectTimeout(dsn string) string {
	if dsn == "" {
		return dsn
	}

	if postgresConnectTimeoutRegex.MatchString(dsn) {
		return dsn
	}

	// Two DSN flavors: URL ("postgres://…") and libpq key=value
	// ("host=… port=… …"). The URL form is detected by the scheme prefix;
	// everything else is treated as key=value.
	if isPostgresURL(dsn) {
		if strings.Contains(dsn, "?") {
			return dsn + "&connect_timeout=10"
		}

		return dsn + "?connect_timeout=10"
	}

	// libpq key=value DSN. Append with a single leading space; libpq
	// tolerates leading/trailing whitespace between pairs.
	if strings.TrimSpace(dsn) == "" {
		return "connect_timeout=10"
	}

	return dsn + " connect_timeout=10"
}

func isPostgresURL(dsn string) bool {
	lower := strings.ToLower(dsn)

	return strings.HasPrefix(lower, "postgres://") || strings.HasPrefix(lower, "postgresql://")
}
