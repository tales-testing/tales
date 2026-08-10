package runtime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tales-testing/tales/internal/model"
	"github.com/tales-testing/tales/internal/parser"
	"github.com/tales-testing/tales/internal/provider"
	"github.com/tales-testing/tales/internal/provider/browser/driver"
)

// loadTalesInDir writes the suite into dir so the test controls where
// upload_file resolves its relative fixture paths from.
func loadTalesInDir(t *testing.T, dir, content string) *model.Suite {
	t.Helper()

	path := filepath.Join(dir, "in.tales")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write tales file: %v", err)
	}

	suite, diags := parser.LoadPath(path)
	if diags.HasErrors() {
		t.Fatalf("load: %s", diags.Error())
	}

	return suite
}

const browserTalesUpload = `version = 1

config {
  browser = {
    targets = {
      chrome = {
        browser  = "chrome"
        headless = true
      }
    }
  }
}

scenario "browser upload" {
  step "browser" "attach" {
    target = "chrome"
    actions {
      upload_file {
        selector = "#document"
        paths    = ["./fixtures/contract.pdf", "./fixtures/appendix.pdf"]
      }
      upload_file {
        selector = "#avatar"
        paths    = "./fixtures/contract.pdf"
      }
    }
  }
}
`

func TestRunnerExecutesBrowserUploadFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	fixtures := filepath.Join(dir, "fixtures")

	if err := os.MkdirAll(fixtures, 0o750); err != nil {
		t.Fatalf("mkdir fixtures: %v", err)
	}

	for _, name := range []string{"contract.pdf", "appendix.pdf"} {
		if err := os.WriteFile(filepath.Join(fixtures, name), []byte(name), 0o600); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
	}

	fake := driver.NewFakeDriver()
	suite := loadTalesInDir(t, dir, browserTalesUpload)
	runner := NewRunner(provider.NewRegistry(newStubBrowserProvider(fake)))

	result, err := runner.Run(context.Background(), suite, Options{Seed: 1234, Parallel: 1})
	if err != nil {
		t.Fatalf("Run returned: %v", err)
	}

	step := result.Scenarios[0].Steps[0]
	if step.Status != "pass" {
		t.Fatalf("step status = %q, want pass; failure=%+v", step.Status, step.Failure)
	}

	uploads := make([]driver.Call, 0, 2)

	for _, c := range fake.Calls {
		if c.Method == "SetFileInputs" {
			uploads = append(uploads, c)
		}
	}

	if len(uploads) != 2 {
		t.Fatalf("expected 2 SetFileInputs calls, got %s", fake.CallsJoined())
	}

	wantList := filepath.Join(fixtures, "contract.pdf") + "," + filepath.Join(fixtures, "appendix.pdf")
	if uploads[0].Value != wantList {
		t.Errorf("list form paths = %q, want %q", uploads[0].Value, wantList)
	}

	// The string form is sugar for a single-entry list.
	if want := filepath.Join(fixtures, "contract.pdf"); uploads[1].Value != want {
		t.Errorf("string form paths = %q, want %q", uploads[1].Value, want)
	}
}

func TestRunnerBrowserUploadFileRejectsBadPaths(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		paths string
		want  string
	}{
		{name: "empty list", paths: "[]", want: "at least one file"},
		{name: "non-string entry", paths: "[42]", want: "every entry must be a string"},
		{name: "wrong type", paths: "42", want: "must be a string or a list of strings"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			content := `version = 1

config {
  browser = {
    targets = {
      chrome = {
        browser  = "chrome"
        headless = true
      }
    }
  }
}

scenario "browser upload" {
  step "browser" "attach" {
    target = "chrome"
    actions {
      upload_file {
        selector = "#document"
        paths    = ` + tc.paths + `
      }
    }
  }
}
`

			fake := driver.NewFakeDriver()
			suite := loadTalesInDir(t, t.TempDir(), content)
			runner := NewRunner(provider.NewRegistry(newStubBrowserProvider(fake)))

			result, err := runner.Run(context.Background(), suite, Options{Seed: 1234, Parallel: 1})
			if err != nil {
				t.Fatalf("Run returned: %v", err)
			}

			step := result.Scenarios[0].Steps[0]
			if step.Status != "fail" {
				t.Fatalf("step status = %q, want fail", step.Status)
			}

			if step.Failure == nil || !strings.Contains(step.Failure.Message, tc.want) {
				t.Fatalf("failure = %+v, want it to mention %q", step.Failure, tc.want)
			}
		})
	}
}
