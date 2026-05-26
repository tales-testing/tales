package sql

import "regexp"

// Mask is the placeholder substituted for any sensitive value.
const Mask = "***"

var (
	// URL-style DSN: scheme://user:password@host/db.
	urlDSNRegex = regexp.MustCompile(`([A-Za-z][A-Za-z0-9+\-.]*://[^:/?#@\s]+):[^@\s]+@`)
	// MySQL go-sql-driver style: user:password@tcp(host:port)/db.
	mysqlDSNRegex = regexp.MustCompile(`([A-Za-z0-9_.\-]+):[^@\s/]+@(tcp|unix)\(`)
	// Generic key=value pair carrying a secret (password, pwd, secret, token).
	paramSecretRegex = regexp.MustCompile(`(?i)\b(password|pwd|secret|token)=([^\s&;]+)`)
)

// MaskDSN replaces credential material in a DSN-like string. It is used only
// to scrub values before they reach error messages or logs; the original DSN
// is never copied into Output.Request.
func MaskDSN(dsn string) string {
	if dsn == "" {
		return dsn
	}

	out := urlDSNRegex.ReplaceAllString(dsn, "$1:"+Mask+"@")
	out = mysqlDSNRegex.ReplaceAllString(out, "$1:"+Mask+"@$2(")
	out = paramSecretRegex.ReplaceAllString(out, "$1="+Mask)

	return out
}
