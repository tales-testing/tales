package parser

import (
	"strings"
	"testing"
)

const rpcConfigPreamble = `version = 1

config {
  rpc = {
    descriptors = { app = { path = "./descriptor.bin" } }
    targets = {
      api = { descriptor = "app", protocol = "connect", base_url = "http://localhost:1337" }
    }
  }
}
`

func TestLoadPathRPCStepHappyPath(t *testing.T) {
	t.Parallel()

	content := rpcConfigPreamble + `
scenario "echo" {
  step "rpc" "echo" {
    target = "api"
    call {
      service = "tales.test.v1.EchoService"
      method  = "Echo"
      message = { text = "hi" }
    }
    expect {
      status  = "ok"
      message = { text = "echo: hi" }
    }
    capture {
      text = response.message.text
    }
  }
}
`

	suite, diags := LoadPath(writeTales(t, content))
	if diags.HasErrors() {
		t.Fatalf("unexpected diagnostics: %s", diags.Error())
	}

	step := suite.Scenarios[0].Steps[0]
	if step.RPC == nil {
		t.Fatal("step.RPC is nil")
	}

	if step.RPC.Service.Empty() || step.RPC.Method.Empty() || step.RPC.Message.Empty() {
		t.Errorf("rpc fields not populated: %+v", step.RPC)
	}

	if step.RPC.Expect == nil || step.RPC.Expect.Status.Empty() || step.RPC.Expect.Message.Empty() {
		t.Errorf("rpc expect not populated: %+v", step.RPC.Expect)
	}
}

func TestLoadPathRPCStepMissingTarget(t *testing.T) {
	t.Parallel()

	content := rpcConfigPreamble + `
scenario "echo" {
  step "rpc" "echo" {
    call {
      service = "tales.test.v1.EchoService"
      method  = "Echo"
    }
  }
}
`

	mustFailLoad(t, content, "Missing rpc target")
}

func TestLoadPathRPCStepMissingCallBlock(t *testing.T) {
	t.Parallel()

	content := rpcConfigPreamble + `
scenario "echo" {
  step "rpc" "echo" {
    target = "api"
  }
}
`

	mustFailLoad(t, content, "Missing rpc call block")
}

func TestLoadPathRPCStepMissingService(t *testing.T) {
	t.Parallel()

	content := rpcConfigPreamble + `
scenario "echo" {
  step "rpc" "echo" {
    target = "api"
    call { method = "Echo" }
  }
}
`

	mustFailLoad(t, content, "Missing rpc service")
}

func TestLoadPathRPCStepMissingMethod(t *testing.T) {
	t.Parallel()

	content := rpcConfigPreamble + `
scenario "echo" {
  step "rpc" "echo" {
    target = "api"
    call { service = "tales.test.v1.EchoService" }
  }
}
`

	mustFailLoad(t, content, "Missing rpc method")
}

func TestLoadPathRPCStepRejectsCallOnNonRPCProvider(t *testing.T) {
	t.Parallel()

	content := rpcConfigPreamble + `
scenario "echo" {
  step "http" "wrong" {
    request {
      method = "GET"
      url    = "http://localhost/"
    }
    call {
      service = "x"
      method  = "Y"
    }
  }
}
`

	mustFailLoad(t, content, "Call block on non-rpc step")
}

func TestLoadPathRPCExpectAllAttributes(t *testing.T) {
	t.Parallel()

	content := rpcConfigPreamble + `
scenario "echo" {
  step "rpc" "echo" {
    target = "api"
    call {
      service = "x"
      method  = "Y"
    }
    expect {
      status   = "invalid_argument"
      error    = { message = "bad" }
      message  = { text = "ok" }
      headers  = { "X-Trace" = "id" }
      metadata = { "X-Md" = "v" }
      trailers = { "X-Tl" = "v" }
    }
  }
}
`

	suite, diags := LoadPath(writeTales(t, content))
	if diags.HasErrors() {
		t.Fatalf("unexpected diagnostics: %s", diags.Error())
	}

	expect := suite.Scenarios[0].Steps[0].RPC.Expect

	if expect == nil || expect.Status.Empty() || expect.Error.Empty() || expect.Message.Empty() || expect.Headers.Empty() || expect.Metadata.Empty() || expect.Trailers.Empty() {
		t.Errorf("expect not fully populated: %+v", expect)
	}
}

func TestLoadPathRPCExpectRejectsHTTPAttrs(t *testing.T) {
	t.Parallel()

	for _, attr := range []string{`json = { foo = 1 }`, `body = "x"`, `strict = true`} {
		content := rpcConfigPreamble + `
scenario "echo" {
  step "rpc" "echo" {
    target = "api"
    call {
      service = "x"
      method  = "Y"
    }
    expect {
      ` + attr + `
    }
  }
}
`
		mustFailLoad(t, content, "Unsupported rpc expect attribute")
	}
}

func TestLoadPathRPCExpectRejectsUnknownAttribute(t *testing.T) {
	t.Parallel()

	content := rpcConfigPreamble + `
scenario "echo" {
  step "rpc" "echo" {
    target = "api"
    call {
      service = "x"
      method  = "Y"
    }
    expect {
      something_random = "boom"
    }
  }
}
`

	mustFailLoad(t, content, "Unknown rpc expect attribute")
}

// mustFailLoad writes content to a temp tales file, runs LoadPath, and
// fails the test unless the diagnostics contain wantContains.
func mustFailLoad(t *testing.T, content, wantContains string) {
	t.Helper()

	_, diags := LoadPath(writeTales(t, content))
	if !diags.HasErrors() {
		t.Fatalf("expected diagnostics containing %q, got success", wantContains)
	}

	if !strings.Contains(diags.Error(), wantContains) {
		t.Fatalf("expected diagnostics containing %q, got:\n%s", wantContains, diags.Error())
	}
}
