package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	"github.com/hyperxlab/tales/internal/cli"
	"github.com/hyperxlab/tales/internal/version"
	urfavecli "github.com/urfave/cli/v3"
)

func main() {
	binName := filepath.Base(os.Args[0])
	app := &urfavecli.Command{
		Name:    binName,
		Usage:   "Declarative integration and e2e testing with .tales files",
		Version: version.Get().Version,
		Commands: []*urfavecli.Command{
			cli.NewTestCommand(),
			cli.NewValidateCommand(),
			cli.NewDoctorCommand(),
		},
		Action: func(ctx context.Context, cmd *urfavecli.Command) error {
			return urfavecli.ShowAppHelp(cmd)
		},
	}
	urfavecli.VersionPrinter = func(cmd *urfavecli.Command) {
		printVersion(cmd.Root().Writer, cmd.Root().Name, version.Get())
	}

	if err := app.Run(context.Background(), os.Args); err != nil {
		log.Fatal(err)
	}
}

func printVersion(w io.Writer, appName string, info *version.Info) {
	_, _ = fmt.Fprintf(w, "%v version: %v (build: %v)\n", appName, info.Version, info.BuildDate)
	_, _ = fmt.Fprintf(w, "commit: %v\n", info.GitCommit)
	_, _ = fmt.Fprintf(w, "Go runtime version: %v\n", info.GoVersion)
	_, _ = fmt.Fprintf(w, "Platform: %v\n", info.Platform)
}
