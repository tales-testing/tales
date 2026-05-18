package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/hyperxlab/tales/internal/parser"
	"github.com/urfave/cli/v3"
)

// NewValidateCommand returns validation command.
func NewValidateCommand() *cli.Command {
	return &cli.Command{
		Name:      "validate",
		Usage:     "Validate .tales files",
		ArgsUsage: "<path>",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			path := "."
			if cmd.NArg() > 0 {
				path = cmd.Args().First()
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
