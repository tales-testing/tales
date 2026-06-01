package cli

import (
	"fmt"
	"io"

	"github.com/hashicorp/hcl/v2"
	"github.com/tales-testing/tales/internal/parser"
)

// printDeprecationWarnings writes one line per deprecation warning to out,
// prefixed with the file:line of the offending block/attribute. It is a no-op
// when no warnings are present so clean suites stay quiet. Warnings never
// affect the exit code; they only nudge users to migrate.
func printDeprecationWarnings(out io.Writer, diags hcl.Diagnostics) {
	warnings := parser.Warnings(diags)
	if len(warnings) == 0 {
		return
	}

	_, _ = fmt.Fprintf(out, "tales: %d deprecation %s:\n", len(warnings), pluralize("warning", len(warnings)))

	for _, warning := range warnings {
		_, _ = fmt.Fprintf(out, "  %s\n", parser.FormatDeprecation(warning))
	}
}
