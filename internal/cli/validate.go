package cli

import (
	"fmt"
	"os"

	"github.com/hyperxlab/tales/internal/parser"
	"github.com/urfave/cli/v2"
)

// NewValidateCommand returns validation command.
func NewValidateCommand() *cli.Command {
	return &cli.Command{
		Name:      "validate",
		Usage:     "Validate .tales files",
		ArgsUsage: "<path>",
		Action: func(c *cli.Context) error {
			path := "."
			if c.NArg() > 0 {
				path = c.Args().First()
			}

			_, diags := parser.LoadPath(path)
			if diags.HasErrors() {
				_, _ = fmt.Fprintf(os.Stderr, "%s\n", diags.Error())

				return cli.Exit("validation failed", 2)
			}

			_, _ = fmt.Fprintf(os.Stdout, "Validation OK\n")

			return nil
		},
	}
}
