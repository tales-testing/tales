package sql

import (
	"context"
	dbsql "database/sql"
	"fmt"

	"github.com/zclconf/go-cty/cty"
)

// scanRows iterates a *sql.Rows, returning a structured representation of the
// query result usable as a cty value via toQueryResponse.
type scannedRow struct {
	columns []string
	values  []cty.Value
}

func scanRows(ctx context.Context, db *dbsql.DB, sqlText string, args []any) ([]scannedRow, []string, error) {
	rows, err := db.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("query failed: %w", err)
	}

	defer func() { _ = rows.Close() }()

	columns, err := rows.Columns()
	if err != nil {
		return nil, nil, fmt.Errorf("read columns: %w", err)
	}

	seen := make(map[string]struct{}, len(columns))

	for _, col := range columns {
		if _, dup := seen[col]; dup {
			return nil, nil, fmt.Errorf("duplicate SQL column %q; use aliases in query", col)
		}

		seen[col] = struct{}{}
	}

	out := make([]scannedRow, 0)

	for rows.Next() {
		holders := make([]any, len(columns))
		pointers := make([]any, len(columns))

		for i := range holders {
			pointers[i] = &holders[i]
		}

		if scanErr := rows.Scan(pointers...); scanErr != nil {
			return nil, nil, fmt.Errorf("scan row: %w", scanErr)
		}

		converted := make([]cty.Value, len(columns))

		for i, raw := range holders {
			value, convErr := ConvertRowValue(raw)
			if convErr != nil {
				return nil, nil, fmt.Errorf("column %q: %w", columns[i], convErr)
			}

			converted[i] = value
		}

		out = append(out, scannedRow{columns: columns, values: converted})
	}

	if iterErr := rows.Err(); iterErr != nil {
		return nil, nil, fmt.Errorf("iterate rows: %w", iterErr)
	}

	return out, columns, nil
}

// toQueryResponse builds a cty Object describing the result of a query step,
// suitable for assignment to provider.Output.Response. The result is wrapped
// under a "json" key so .tales scenarios can write `expect { json = {...} }`
// and `capture { x = response.json.rows[0].y }` consistently with the HTTP
// provider.
func toQueryResponse(rows []scannedRow, columns []string) map[string]cty.Value {
	rowValues := make([]cty.Value, 0, len(rows))

	for _, row := range rows {
		attrs := make(map[string]cty.Value, len(row.columns))
		for i, col := range row.columns {
			attrs[col] = row.values[i]
		}

		rowValues = append(rowValues, cty.ObjectVal(attrs))
	}

	columnValues := make([]cty.Value, 0, len(columns))
	for _, col := range columns {
		columnValues = append(columnValues, cty.StringVal(col))
	}

	rowsList := cty.EmptyTupleVal
	if len(rowValues) > 0 {
		rowsList = cty.TupleVal(rowValues)
	}

	columnsList := cty.EmptyTupleVal
	if len(columnValues) > 0 {
		columnsList = cty.TupleVal(columnValues)
	}

	jsonValue := cty.ObjectVal(map[string]cty.Value{
		"row_count": cty.NumberIntVal(int64(len(rows))),
		"columns":   columnsList,
		"rows":      rowsList,
	})

	return map[string]cty.Value{"json": jsonValue}
}

// toExecResponse builds the cty.Value map for an exec step. RowsAffected /
// LastInsertId driver errors map to null so suites can assert with optional
// or any() matchers. Like toQueryResponse, the payload is exposed under the
// "json" key.
func toExecResponse(result dbsql.Result) map[string]cty.Value {
	rowsAffected := cty.NullVal(cty.Number)

	if result != nil {
		if ra, err := result.RowsAffected(); err == nil {
			rowsAffected = cty.NumberIntVal(ra)
		}
	}

	lastInsertID := cty.NullVal(cty.Number)

	if result != nil {
		if id, err := result.LastInsertId(); err == nil {
			lastInsertID = cty.NumberIntVal(id)
		}
	}

	jsonValue := cty.ObjectVal(map[string]cty.Value{
		"rows_affected":  rowsAffected,
		"last_insert_id": lastInsertID,
	})

	return map[string]cty.Value{"json": jsonValue}
}
