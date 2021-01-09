package main

import (
	"log"
	"os"

	"github.com/hyperxlab/tales/pkg/tales/configs"
	"github.com/zclconf/go-cty/cty"
)

func main() {
	parser := configs.NewParser(nil)

	pwd, err := os.Getwd()
	if err != nil {
		log.Fatalf("Failed to get current dir: %s", err)
		os.Exit(1)
	}

	module, diags := parser.LoadConfigDir(pwd)

	if diags.HasErrors() {
		log.Fatalf("error in load config dir: %s", diags.Error())
	}
	//spew.Dump(module)
	//spew.Dump(diags)

	for _, s := range module.Scenarios {
		log.Printf("Scenario %s\n", s.Name)

		ctx := parser.Context().NewChild()

		ctx.Variables = map[string]cty.Value{
			"http":    cty.ObjectVal(map[string]cty.Value{}),
			"keyword": cty.ObjectVal(map[string]cty.Value{}),
		}

		s.Execute(module, ctx)
		/*
			lastStatus := configs.StatusPassed

			for _, c := range s.CasesConfig {
				switch c.Type {
				case "keyword":
					keyword, ok := module.Keywords[c.Name]
					if !ok {
						log.Fatalf("keyword %q not exists", c.Name)
					}

					testCase := &configs.KeywordCase{
						Keyword: keyword,
					}

					if lastStatus == configs.StatusPassed {
						if diag := testCase.Parse(c, ctx); diag.HasErrors() {
							log.Fatalf("error in DecodeBody decoding HCL keyword case: %s", diag.Error())
						}

						result := testCase.Execute(ctx)
						lastStatus = result.Status
					} else {
						lastStatus = configs.StatusNotExecuted
					}

					s.Cases = append(s.Cases, testCase)

					log.Printf("\tCase %s %s in %s\n", testCase.Result().Name, testCase.Result().Status, testCase.Result().Duration)
					if testCase.Result().Status == configs.StatusFailed {
						log.Printf("\t\tError: %s\n", testCase.Result().Raison)
					}

				case "http":
					testCase := &configs.HTTPCase{}

					if lastStatus == configs.StatusPassed {
						if diag := testCase.Parse(c, ctx); diag.HasErrors() {
							log.Fatalf("error in DecodeBody decoding HCL http case: %s", diag.Error())
						}

						result := testCase.Execute(ctx)
						lastStatus = result.Status
					} else {
						lastStatus = configs.StatusNotExecuted
					}

					s.Cases = append(s.Cases, testCase)

					log.Printf("\tCase %s %s in %s\n", testCase.Result().Name, testCase.Result().Status, testCase.Result().Duration)
					if testCase.Result().Status == configs.StatusFailed {
						log.Printf("\t\tError: %s\n", testCase.Result().Raison)
					}
				}
			}*/
	}
}

/**


var scenario ScenarioConfigFile

evalContext, err := createContext()
if err != nil {
	log.Fatalf("Failed to create context: %s", err)
}

ctx = evalContext

if err := DecodeFile(os.Args[1], ctx, &scenario); err != nil {
	log.Fatalf("Failed to load scenario: %s", err)
}

for _, s := range scenario.Scenarios {
	log.Printf("Scenario %s\n", s.Name)

	ctx = ctx.NewChild()

	ctx.Variables = map[string]cty.Value{
		"http": cty.ObjectVal(map[string]cty.Value{}),
	}

	lastStatus := StatusPassed

	for _, c := range s.CasesConfig {
		switch c.Type {
		case "http":
			testCase := &HTTPCase{
				Name: c.Name,
			}
			if lastStatus == StatusPassed {
				if diag := gohcl.DecodeBody(c.HCL, ctx, testCase); diag.HasErrors() {
					log.Fatalf("error in DecodeBody decoding HCL http case: %s", diag.Error())
				}

				result := testCase.Execute(ctx)
				lastStatus = result.Status
			} else {
				lastStatus = StatusNotExecuted

				testCase.result = Result{
					Name:   c.Name,
					Status: StatusNotExecuted,
				}
			}

			s.Cases = append(s.Cases, testCase)

			log.Printf("\tCase %s %s in %s\n", testCase.Result().Name, testCase.Result().Status, testCase.Result().Duration)
			if testCase.Result().Status == StatusFailed {
				log.Printf("\t\tError: %s\n", testCase.Result().Raison)
			}
		}
	}
}
*/
/*
	for _, s := range scenario.Scenarios {
		log.Printf("Scenario %s\n", s.Name)

		for _, c := range s.Cases {
			log.Printf("\tCase %s %s in %s\n", c.Result().Name, c.Result().Status, c.Result().Duration)
			if c.Result().Status == StatusFailed {
				log.Printf("\t\tError: %s\n", c.Result().Raison)
			}
		}
	}
*/
