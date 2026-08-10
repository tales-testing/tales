package sql

import (
	"strconv"
	"strings"
)

// hasPrefixAt reports whether sql contains prefix starting at i.
func hasPrefixAt(sql string, i int, prefix string) bool {
	return strings.HasPrefix(sql[i:], prefix)
}

// indexAt returns the offset of the first occurrence of needle at or after
// from, or -1.
func indexAt(sql string, from int, needle string) int {
	if from >= len(sql) {
		return -1
	}

	found := strings.Index(sql[from:], needle)
	if found < 0 {
		return -1
	}

	return from + found
}

// scanPlaceholders returns every driver placeholder in sql, in textual order,
// skipping string literals, quoted identifiers, line / block comments and, for
// postgres, dollar-quoted bodies. Without those exclusions a `?` inside a
// string or a `$2` inside a comment would shift the whole renumbering.
//
// The scan is byte-wise: every delimiter it looks for is ASCII and UTF-8
// continuation bytes are all >= 0x80, so multi-byte text can never produce a
// false match, and only whole slices are ever copied.
//
// Known limitation: MySQL executes optimizer hints written as /*! ... */, but
// they are skipped here like any other block comment. A placeholder inside one
// would therefore go unnoticed.
func scanPlaceholders(d dialect, sql string) ([]placeholder, error) {
	found := make([]placeholder, 0, 4)
	i := 0

	for i < len(sql) {
		if next, skipped := skipNonCode(d, sql, i); skipped {
			i = next

			continue
		}

		if d.numbered && sql[i] == '$' {
			p, ok := readNumbered(sql, i)
			if !ok {
				i++

				continue
			}

			found = append(found, p)
			i = p.end

			continue
		}

		if d.positional && sql[i] == '?' {
			found = append(found, placeholder{start: i, end: i + 1, index: len(found) + 1})
			i++

			continue
		}

		i++
	}

	return found, nil
}

// skipNonCode advances past a comment, a quoted literal / identifier or a
// dollar-quoted body starting at i. It returns the index just past the region
// and true when it consumed one, or (i, false) when sql[i] starts real code.
func skipNonCode(d dialect, sql string, i int) (int, bool) {
	switch {
	case isLineCommentStart(d, sql, i):
		return skipLineComment(sql, i), true
	case hasPrefixAt(sql, i, "/*"):
		return skipBlockComment(sql, i, d.nestedBlockComments), true
	case sql[i] == '\'':
		return skipQuoted(sql, i, '\'', d.backslashEscapes || isPostgresEscapeString(d, sql, i)), true
	case sql[i] == '"':
		return skipQuoted(sql, i, '"', d.backslashEscapes), true
	case d.backtickIdent && sql[i] == '`':
		return skipQuoted(sql, i, '`', false), true
	case d.dollarQuoting && sql[i] == '$':
		return skipDollarQuoted(sql, i)
	default:
		return i, false
	}
}

// isLineCommentStart reports whether a line comment opens at i. MySQL requires
// a blank after "--", so `WHERE a=1--?` binds a placeholder there while the
// same text is a comment in postgres.
func isLineCommentStart(d dialect, sql string, i int) bool {
	if d.hashLineComment && sql[i] == '#' {
		return true
	}

	if !hasPrefixAt(sql, i, "--") {
		return false
	}

	if !d.dashDashNeedsSpace {
		return true
	}

	if i+2 >= len(sql) {
		return true
	}

	return sql[i+2] == ' ' || sql[i+2] == '\t' || sql[i+2] == '\n' || sql[i+2] == '\r'
}

func skipLineComment(sql string, i int) int {
	for j := i; j < len(sql); j++ {
		if sql[j] == '\n' {
			return j + 1
		}
	}

	return len(sql)
}

// skipBlockComment consumes /* ... */. Postgres nests them, MySQL does not.
func skipBlockComment(sql string, i int, nested bool) int {
	depth := 1
	j := i + 2

	for j < len(sql) {
		if hasPrefixAt(sql, j, "*/") {
			depth--
			j += 2

			if depth == 0 {
				return j
			}

			continue
		}

		if nested && hasPrefixAt(sql, j, "/*") {
			depth++
			j += 2

			continue
		}

		j++
	}

	return len(sql)
}

// skipQuoted consumes a quoted literal or identifier, honoring the doubled
// quote convention (” inside '...') and, when backslash is set, backslash
// escapes.
func skipQuoted(sql string, i int, quote byte, backslash bool) int {
	j := i + 1

	for j < len(sql) {
		if backslash && sql[j] == '\\' && j+1 < len(sql) {
			j += 2

			continue
		}

		if sql[j] != quote {
			j++

			continue
		}

		if j+1 < len(sql) && sql[j+1] == quote {
			j += 2

			continue
		}

		return j + 1
	}

	return len(sql)
}

// isPostgresEscapeString reports whether the literal opening at i is an E'...'
// escape string, in which case backslashes escape. The E must stand alone: a
// trailing E of an identifier (e.g. VALUE'x') is not an escape marker.
func isPostgresEscapeString(d dialect, sql string, i int) bool {
	if !d.dollarQuoting || i == 0 {
		return false
	}

	if sql[i-1] != 'E' && sql[i-1] != 'e' {
		return false
	}

	if i == 1 {
		return true
	}

	return !isIdentByte(sql[i-2])
}

func isIdentByte(b byte) bool {
	return b == '_' ||
		(b >= '0' && b <= '9') ||
		(b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z')
}

// skipDollarQuoted consumes a $tag$ ... $tag$ body (tag may be empty, giving
// $$ ... $$). It must be tried before readNumbered: without it, `$$body $1$$`
// would yield a phantom placeholder.
func skipDollarQuoted(sql string, i int) (int, bool) {
	tagEnd := i + 1
	for tagEnd < len(sql) && isIdentByte(sql[tagEnd]) && !isDigitByte(sql[tagEnd]) {
		tagEnd++
	}

	if tagEnd >= len(sql) || sql[tagEnd] != '$' {
		return i, false
	}

	tag := sql[i : tagEnd+1]

	closing := indexAt(sql, tagEnd+1, tag)
	if closing < 0 {
		return len(sql), true
	}

	return closing + len(tag), true
}

func isDigitByte(b byte) bool {
	return b >= '0' && b <= '9'
}

// readNumbered parses a $N placeholder at i.
func readNumbered(sql string, i int) (placeholder, bool) {
	j := i + 1
	for j < len(sql) && isDigitByte(sql[j]) {
		j++
	}

	if j == i+1 {
		return placeholder{}, false
	}

	n, err := strconv.Atoi(sql[i+1 : j])
	if err != nil {
		return placeholder{}, false
	}

	return placeholder{start: i, end: j, index: n}, true
}
