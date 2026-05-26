package sql

// Blank imports register database/sql drivers used by the SQL provider.
// Kept in their own file so the rest of the package compiles without the
// drivers when tests inject a custom database/sql driver (e.g. sqlmock).
import (
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
)
