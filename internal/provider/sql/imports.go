package sql

// Blank imports register database/sql drivers used by the SQL provider.
// They live in their own file so the dependency on the concrete drivers
// is isolated from the rest of the package source — handy when grepping
// for imports during reviews, and a single touch point if Tales ever
// gates extra drivers behind build tags.
import (
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
)
