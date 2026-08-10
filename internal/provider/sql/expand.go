package sql

import (
	"fmt"
	"strconv"
	"strings"
)

// dialect describes the placeholder and literal syntax of one SQL flavor, so
// the scanner stays a single implementation driven by data.
type dialect struct {
	// numbered marks $N placeholders (postgres / pgx); positional marks ?
	// placeholders (mysql). Exactly one is true.
	numbered   bool
	positional bool
	// dollarQuoting enables $tag$ ... $tag$ bodies (postgres).
	dollarQuoting bool
	// nestedBlockComments enables /* /* */ */ nesting (postgres).
	nestedBlockComments bool
	// backtickIdent enables `ident` quoting (mysql).
	backtickIdent bool
	// hashLineComment enables # ... line comments (mysql).
	hashLineComment bool
	// dashDashNeedsSpace requires "-- " (not "--") to open a line comment,
	// which is MySQL's rule: `a=1--?` is not a comment there.
	dashDashNeedsSpace bool
	// backslashEscapes enables \' escaping inside quoted strings. MySQL does
	// it by default; postgres only inside E'...' literals, handled separately.
	backslashEscapes bool
}

// dialectFor maps a user-facing driver alias to its dialect.
func dialectFor(alias string) (dialect, error) {
	driver, err := resolveDriver(alias)
	if err != nil {
		return dialect{}, err
	}

	switch driver {
	case databaseDriverPgx:
		return dialect{numbered: true, dollarQuoting: true, nestedBlockComments: true}, nil
	case databaseDriverMySQL:
		return dialect{positional: true, backtickIdent: true, hashLineComment: true, dashDashNeedsSpace: true, backslashEscapes: true}, nil
	default:
		return dialect{}, fmt.Errorf("unsupported sql driver %q", alias)
	}
}

// placeholder is one driver placeholder located in a statement.
type placeholder struct {
	start int // byte offset of '$' or '?'
	end   int // byte offset just past the placeholder
	index int // 1-based: the parsed N for $N, the textual ordinal for ?
}

// expandListArgs rewrites sql so every placeholder bound to a ListArg becomes
// a comma-separated run of placeholders (IN ($1,$2,$3) / IN (?,?,?)) and
// returns the flattened argument list in binding order.
//
// It is a strict no-op (same string, same slice) when no argument is a list,
// so statements that only bind scalars keep their exact current behavior and
// never pay for the scan.
func expandListArgs(driverAlias, sql string, args []any) (string, []any, error) {
	if !hasListArg(args) {
		return sql, args, nil
	}

	d, err := dialectFor(driverAlias)
	if err != nil {
		return "", nil, err
	}

	found, err := scanPlaceholders(d, sql)
	if err != nil {
		return "", nil, err
	}

	if d.numbered {
		return expandNumbered(d, sql, args, found)
	}

	return expandPositional(d, sql, args, found)
}

// hasListArg reports whether any argument was written as a list.
func hasListArg(args []any) bool {
	for _, arg := range args {
		if _, ok := arg.(ListArg); ok {
			return true
		}
	}

	return false
}

// argWidth returns how many placeholders an argument occupies once expanded.
func argWidth(arg any) int {
	if list, ok := arg.(ListArg); ok {
		return len(list)
	}

	return 1
}

// flattenArgs returns args with every ListArg spliced in place.
func flattenArgs(args []any) []any {
	out := make([]any, 0, len(args))

	for _, arg := range args {
		list, ok := arg.(ListArg)
		if !ok {
			out = append(out, arg)

			continue
		}

		out = append(out, list...)
	}

	return out
}

// emptyListError explains why an empty list cannot be expanded. SQL has no
// valid form for "IN ()", and substituting NULL would silently change the
// meaning of the statement (correct for IN, wrong for NOT IN), which a testing
// tool must never do behind the user's back.
func emptyListError(index int) error {
	return fmt.Errorf("args[%d] is an empty list; SQL has no valid form for \"IN ()\". Guard the step with `when`, or pass a sentinel value", index)
}

// expandNumbered rewrites $N placeholders. Each argument N reserves a run of
// width w(N) (1 for a scalar, len(list) for a list), so the new base index of
// argument N is 1 + sum(w(k)) for every k < N and every later placeholder
// shifts. Rewriting by byte offsets keeps this correct even when the
// placeholders appear out of order in the text.
func expandNumbered(d dialect, sql string, args []any, found []placeholder) (string, []any, error) {
	counts := make(map[int]int, len(args))

	for _, p := range found {
		if p.index < 1 || p.index > len(args) {
			return "", nil, fmt.Errorf("statement references $%d but only %d argument(s) were provided", p.index, len(args))
		}

		counts[p.index]++
	}

	bases := make([]int, len(args)+1)
	next := 1

	for i, arg := range args {
		if err := checkNumberedListArg(i, arg, counts[i+1]); err != nil {
			return "", nil, err
		}

		bases[i+1] = next
		next += argWidth(arg)
	}

	return rewrite(sql, found, func(p placeholder) string {
		return placeholderRun(d, bases[p.index], argWidth(args[p.index-1]))
	}), flattenArgs(args), nil
}

// checkNumberedListArg rejects the list bindings that cannot be renumbered
// unambiguously. A reused scalar placeholder stays legal: both occurrences
// simply get the same new base.
func checkNumberedListArg(index int, arg any, occurrences int) error {
	list, ok := arg.(ListArg)
	if !ok {
		return nil
	}

	if len(list) == 0 {
		return emptyListError(index)
	}

	if occurrences == 0 {
		return fmt.Errorf("args[%d] is a list but $%d does not appear in the statement", index, index+1)
	}

	if occurrences > 1 {
		return fmt.Errorf("list argument $%d is used %d times; a list placeholder may appear only once", index+1, occurrences)
	}

	return nil
}

// expandPositional rewrites ? placeholders: the k-th placeholder binds
// args[k], so the mapping is only well defined when the counts match.
func expandPositional(d dialect, sql string, args []any, found []placeholder) (string, []any, error) {
	if len(found) != len(args) {
		return "", nil, fmt.Errorf("statement has %d placeholder(s) but %d argument(s) were provided", len(found), len(args))
	}

	for i, arg := range args {
		if list, ok := arg.(ListArg); ok && len(list) == 0 {
			return "", nil, emptyListError(i)
		}
	}

	return rewrite(sql, found, func(p placeholder) string {
		return placeholderRun(d, 0, argWidth(args[p.index-1]))
	}), flattenArgs(args), nil
}

// rewrite replaces every located placeholder with the text render returns for
// it, copying the rest of the statement verbatim.
func rewrite(sql string, found []placeholder, render func(placeholder) string) string {
	builder := strings.Builder{}
	builder.Grow(len(sql))

	cursor := 0

	for _, p := range found {
		builder.WriteString(sql[cursor:p.start])
		builder.WriteString(render(p))

		cursor = p.end
	}

	builder.WriteString(sql[cursor:])

	return builder.String()
}

// placeholderRun renders "$base,$base+1,..." for a numbered dialect or
// "?,?,..." for a positional one.
func placeholderRun(d dialect, base, width int) string {
	parts := make([]string, 0, width)

	for i := range width {
		if d.numbered {
			parts = append(parts, "$"+strconv.Itoa(base+i))

			continue
		}

		parts = append(parts, "?")
	}

	return strings.Join(parts, ",")
}
