package main

import (
	"fmt"
	"os"

	"github.com/hyperxlab/tales/pkg/tales/reporter"

	"github.com/hyperxlab/tales/pkg/tales/configs"
	_ "github.com/hyperxlab/tales/pkg/tales/generators"
	"github.com/urfave/cli/v2"
	"github.com/zclconf/go-cty/cty"
)

func rootCmd(c *cli.Context) error {
	reporter, err := reporter.NewManager(c.StringSlice("report"))
	if err != nil {
		return fmt.Errorf("reporter: %w", err)
	}

	if err := reporter.Start(); err != nil {
		return fmt.Errorf("reporter start failed: %w", err)
	}

	parser := configs.NewParser(nil)

	configDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("Failed to get current dir: %w", err)
	}

	if c.NArg() >= 1 {
		configDir = c.Args().First()
	}

	module, diags := parser.LoadConfigDir(configDir)

	if diags.HasErrors() {
		return fmt.Errorf("error in load config dir: %w", diags)
	}

	module.Reporter = reporter

	tags := c.StringSlice("tags")

	for _, g := range module.Generators {
		ctx := parser.Context().NewChild()

		if diags := g.Execute(module, ctx); diags.HasErrors() {
			return fmt.Errorf("error in load config dir: %w", diags)
		}
	}

	for _, s := range module.Scenarios {
		if !s.HasTags(tags) {
			continue
		}

		ctx := parser.Context().NewChild()

		ctx.Variables = map[string]cty.Value{
			"http":    cty.ObjectVal(map[string]cty.Value{}),
			"keyword": cty.ObjectVal(map[string]cty.Value{}),
		}

		s.Execute(module, ctx)
	}

	return reporter.Stop()
}
