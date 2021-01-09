package configs

import (
	"log"

	"github.com/hashicorp/hcl/v2"
)

// Scenario struct
type Scenario struct {
	Name        string   `hcl:"name"`
	Tags        []string `hcl:"tags,optional"`
	CasesConfig []*Case  `hcl:"case,block"`
	Cases       []TestCase
}

// Execute Scenario
func (s *Scenario) Execute(module *Module, ctx *hcl.EvalContext) {
	lastStatus := StatusPassed

	for _, c := range s.CasesConfig {
		testCase := engineExec(module, c, ctx)

		if lastStatus == StatusPassed {
			lastStatus = testCase.Execute(ctx).Status
		} else {
			lastStatus = StatusNotExecuted
		}

		s.Cases = append(s.Cases, testCase)

		log.Printf("\tCase %s %s in %s\n", testCase.Result().Name, testCase.Result().Status, testCase.Result().Duration)
		if testCase.Result().Status == StatusFailed {
			log.Printf("\t\tError: %s\n", testCase.Result().Raison)
		}
	}
}
