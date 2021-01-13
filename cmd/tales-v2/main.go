package main

import (
	"os"

	"github.com/davecgh/go-spew/spew"
	"github.com/hyperxlab/tales/pkg/tales/configs"
)

func main() {

	parser := configs.NewParser(nil)

	file, diags := parser.LoadConfigFile(os.Args[1])

	spew.Dump(file)
	spew.Dump(diags)
}
