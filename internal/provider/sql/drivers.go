package sql

import "fmt"

// Driver aliases accepted by .tales config.sql.connections.<name>.driver.
const (
	driverAliasPostgres = "postgres"
	driverAliasPgx      = "pgx"
	driverAliasMySQL    = "mysql"
)

// Underlying driver names registered with database/sql.
const (
	databaseDriverPgx   = "pgx"
	databaseDriverMySQL = "mysql"
)

// resolveDriver maps a user-supplied driver alias to the database/sql driver
// name that Tales actually uses. Unknown aliases return an error.
func resolveDriver(alias string) (string, error) {
	switch alias {
	case driverAliasPostgres, driverAliasPgx:
		return databaseDriverPgx, nil
	case driverAliasMySQL:
		return databaseDriverMySQL, nil
	default:
		return "", fmt.Errorf("unsupported sql driver %q", alias)
	}
}
