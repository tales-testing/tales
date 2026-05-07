package report

import (
	"encoding/xml"
	"fmt"
	"os"
	"time"
)

type testsuiteXML struct {
	XMLName   xml.Name      `xml:"testsuite"`
	Name      string        `xml:"name,attr"`
	Tests     int           `xml:"tests,attr"`
	Failures  int           `xml:"failures,attr"`
	Time      string        `xml:"time,attr"`
	TestCases []testcaseXML `xml:"testcase"`
}

type testcaseXML struct {
	Name      string      `xml:"name,attr"`
	ClassName string      `xml:"classname,attr"`
	Time      string      `xml:"time,attr"`
	Failure   *failureXML `xml:"failure,omitempty"`
}

type failureXML struct {
	Message string `xml:"message,attr"`
	Body    string `xml:",chardata"`
}

// WriteJUnit writes junit xml report with one testcase per scenario.
func WriteJUnit(path string, result *SuiteResult) error {
	x := testsuiteXML{Name: "tales", Tests: len(result.Scenarios), Time: seconds(result.Duration)}
	for _, scenario := range result.Scenarios {
		tc := testcaseXML{
			Name:      scenario.Name,
			ClassName: scenario.File,
			Time:      seconds(scenario.Duration),
		}
		if scenario.Status == StatusFail {
			x.Failures++
			message := "scenario failed"
			body := ""
			if scenario.Failure != nil {
				message = scenario.Failure.Message
				body = fmt.Sprintf("kind=%s path=%s", scenario.Failure.Kind, scenario.Failure.Path)
			}
			tc.Failure = &failureXML{Message: message, Body: body}
		}
		x.TestCases = append(x.TestCases, tc)
	}

	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	enc := xml.NewEncoder(file)
	enc.Indent("", "  ")
	if err := enc.Encode(x); err != nil {
		return err
	}
	return nil
}

func seconds(d time.Duration) string {
	return fmt.Sprintf("%.3f", d.Seconds())
}
