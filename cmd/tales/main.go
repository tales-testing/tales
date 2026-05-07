package main

import (
	"fmt"
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
		},
		Action: urfavecli.ShowAppHelp,
	}
	urfavecli.VersionPrinter = func(c *urfavecli.Context) {
		_, _ = fmt.Fprintf(c.App.Writer, "%v version: %v (build: %v)\n", c.App.Name, c.App.Version, version.Get().BuildDate)
		_, _ = fmt.Fprintf(c.App.Writer, "Go runtime version: %v\n", version.Get().GoVersion)
		_, _ = fmt.Fprintf(c.App.Writer, "Platform: %v\n", version.Get().Platform)
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}
