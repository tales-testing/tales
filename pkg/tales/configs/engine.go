package configs

import (
	"log"

	"github.com/hashicorp/hcl/v2"
)

func engineExec(module *Module, c *Case, ctx *hcl.EvalContext) TestCase {
	switch c.Type {
	case "keyword":
		keyword, ok := module.Keywords[c.Name]
		if !ok {
			log.Fatalf("keyword %q not exists", c.Name)
		}

		testCase := &KeywordCase{
			Module:  module,
			Keyword: keyword,
		}

		if diag := testCase.Parse(c, ctx); diag.HasErrors() {
			log.Fatalf("error in DecodeBody decoding HCL keyword case: %s", diag.Error())
		}

		return testCase

	case "http":
		testCase := &HTTPCase{}

		if diag := testCase.Parse(c, ctx); diag.HasErrors() {
			log.Fatalf("error in DecodeBody decoding HCL http case: %s", diag.Error())
		}

		return testCase
	}

	return nil

}
