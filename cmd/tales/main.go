package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	"github.com/hyperxlab/tales/internal/cli"
	"github.com/hyperxlab/tales/internal/version"
	urfavecli "github.com/urfave/cli/v2"
)

func main() {
	binName := filepath.Base(os.Args[0])
	app := &urfavecli.App{
		Name:    binName,
		Usage:   "Declarative integration and e2e testing with .tales files",
		Version: version.Get().Version,
		Commands: []*urfavecli.Command{
			cli.NewTestCommand(),
			cli.NewValidateCommand(),
			cli.NewDoctorCommand(),
		},
		Action: urfavecli.ShowAppHelp,
	}
	urfavecli.VersionPrinter = func(c *urfavecli.Context) {
		printVersion(c.App.Writer, c.App.Name, version.Get())
	}

	if err := app.Run(reorderArgs(os.Args, collectBoolFlags(app))); err != nil {
		log.Fatal(err)
	}
}

func printVersion(w io.Writer, appName string, info *version.Info) {
	_, _ = fmt.Fprintf(w, "%v version: %v (build: %v)\n", appName, info.Version, info.BuildDate)
	_, _ = fmt.Fprintf(w, "commit: %v\n", info.GitCommit)
	_, _ = fmt.Fprintf(w, "Go runtime version: %v\n", info.GoVersion)
	_, _ = fmt.Fprintf(w, "Platform: %v\n", info.Platform)
}
