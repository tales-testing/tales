package configs

import (
	"log"

	"github.com/hashicorp/hcl/v2"
	"github.com/hyperxlab/tales/pkg/tales/reporter"
)

// Scenario struct
type Scenario struct {
	Name        string   `hcl:"name"`
	Tags        []string `hcl:"tags,optional"`
	CasesConfig []*Case  `hcl:"case,block"`
	Cases       []TestCase
}

// HasTags return true if one of tags is present in Scenario.Tags
func (s *Scenario) HasTags(tags []string) bool {
	if len(tags) == 0 {
		return true
	}

	for _, tag := range s.Tags {
		for _, t := range tags {
			if t == tag {
				return true
			}
		}
	}

	return false
}

// Execute Scenario
func (s *Scenario) Execute(module *Module, ctx *hcl.EvalContext) {
	lastStatus := reporter.StatusPassed

	if err := module.Reporter.ReportScenario(&reporter.Scenario{
		Name: s.Name,
		Tags: s.Tags,
	}); err != nil {
		log.Panic(err)
	}

	for _, c := range s.CasesConfig {
		testCase := engineExec(module, c, ctx)

		if lastStatus == reporter.StatusPassed {
			lastStatus = testCase.Execute(ctx).Status
		} else {
			lastStatus = reporter.StatusNotExecuted
		}

		s.Cases = append(s.Cases, testCase)

		if err := module.Reporter.ReportCase(testCase.Result()); err != nil {
			log.Panic(err)
		}
	}
}
