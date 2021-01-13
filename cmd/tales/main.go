package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"

	"github.com/hyperxlab/tales/pkg/tales/version"
	"github.com/urfave/cli/v2"
)

func main() {
	cli.VersionPrinter = func(c *cli.Context) {
		_, _ = fmt.Fprintf(c.App.Writer, "%v version: %v (build: %v)\n", c.App.Name, c.App.Version, version.Get().BuildDate)
		_, _ = fmt.Fprintf(c.App.Writer, "Go runtime version: %v\n", version.Get().GoVersion)
		_, _ = fmt.Fprintf(c.App.Writer, "Platform: %v\n", version.Get().Platform)
	}

	// Get the command line args.
	binName := filepath.Base(os.Args[0])

	app := &cli.App{
		Name:    binName,
		Version: version.Get().Version,
		//Description: "Generic automation framework for acceptance testing.",
		Usage:     "Generic automation framework for acceptance testing.",
		Copyright: "Axel Etcheverry",
		Flags: []cli.Flag{
			&cli.StringSliceFlag{
				Name:    "tags",
				Aliases: []string{"t"},
				Usage:   "Filter by tags name",
			},
			&cli.StringSliceFlag{
				Name:    "report",
				Aliases: []string{"r"},
				Usage:   "name of reporter",
				Value:   cli.NewStringSlice("console"),
			},
		},
		Action: rootCmd,
	}

	sort.Sort(cli.FlagsByName(app.Flags))
	sort.Sort(cli.CommandsByName(app.Commands))

	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}
