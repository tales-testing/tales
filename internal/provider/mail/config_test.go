package mail

import (
	"strings"
	"testing"
	"time"

	"github.com/zclconf/go-cty/cty"
)

func TestResolveTargetSMTP(t *testing.T) {
	t.Parallel()

	cfg := mailConfig("inbound", map[string]cty.Value{
		"protocol": cty.StringVal("smtp"),
		"host":     cty.StringVal("127.0.0.1"),
		"port":     cty.NumberIntVal(2525),
		"tls":      cty.BoolVal(false),
	})

	target, err := resolveTarget(cfg, "inbound")
	if err != nil {
		t.Fatalf("resolveTarget: %v", err)
	}

	if target.Protocol != "smtp" || target.Host != "127.0.0.1" || target.Port != 2525 {
		t.Fatalf("unexpected target: %+v", target)
	}

	if target.Timeout != 10*time.Second {
		t.Fatalf("default timeout should be 10s, got %s", target.Timeout)
	}
}

func TestResolveTargetLMTP(t *testing.T) {
	t.Parallel()

	cfg := mailConfig("ingest", map[string]cty.Value{
		"protocol": cty.StringVal("lmtp"),
		"network":  cty.StringVal("unix"),
		"address":  cty.StringVal("/tmp/ingest.sock"),
		"timeout":  cty.StringVal("5s"),
	})

	target, err := resolveTarget(cfg, "ingest")
	if err != nil {
		t.Fatalf("resolveTarget: %v", err)
	}

	if target.Network != "unix" || target.Address != "/tmp/ingest.sock" {
		t.Fatalf("unexpected lmtp target: %+v", target)
	}

	if target.Timeout != 5*time.Second {
		t.Fatalf("timeout override should be 5s, got %s", target.Timeout)
	}
}

func TestResolveTargetErrors(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		name    string
		cfg     map[string]cty.Value
		wantSub string
	}{
		"missing target": {
			name:    "nope",
			cfg:     mailConfig("inbound", map[string]cty.Value{"protocol": cty.StringVal("smtp"), "host": cty.StringVal("h"), "port": cty.NumberIntVal(25)}),
			wantSub: `mail target "nope" not found`,
		},
		"unsupported protocol": {
			name:    "inbound",
			cfg:     mailConfig("inbound", map[string]cty.Value{"protocol": cty.StringVal("imap")}),
			wantSub: `unsupported mail protocol "imap"; supported protocols: smtp, lmtp`,
		},
		"empty smtp host": {
			name:    "inbound",
			cfg:     mailConfig("inbound", map[string]cty.Value{"protocol": cty.StringVal("smtp"), "port": cty.NumberIntVal(25)}),
			wantSub: `mail target "inbound" has empty SMTP host`,
		},
		"empty smtp port": {
			name:    "inbound",
			cfg:     mailConfig("inbound", map[string]cty.Value{"protocol": cty.StringVal("smtp"), "host": cty.StringVal("h")}),
			wantSub: `mail target "inbound" has empty SMTP port`,
		},
		"unsupported lmtp network": {
			name:    "ingest",
			cfg:     mailConfig("ingest", map[string]cty.Value{"protocol": cty.StringVal("lmtp"), "network": cty.StringVal("udp"), "address": cty.StringVal("a")}),
			wantSub: `mail target "ingest" has unsupported LMTP network "udp"`,
		},
		"empty lmtp address": {
			name:    "ingest",
			cfg:     mailConfig("ingest", map[string]cty.Value{"protocol": cty.StringVal("lmtp"), "network": cty.StringVal("tcp")}),
			wantSub: `mail target "ingest" has empty LMTP address`,
		},
		"tls and starttls": {
			name: "inbound",
			cfg: mailConfig("inbound", map[string]cty.Value{
				"protocol": cty.StringVal("smtp"), "host": cty.StringVal("h"), "port": cty.NumberIntVal(25),
				"tls": cty.BoolVal(true), "starttls": cty.BoolVal(true),
			}),
			wantSub: "cannot enable both tls and starttls",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := resolveTarget(tc.cfg, tc.name)
			if err == nil {
				t.Fatalf("expected error for %q", name)
			}

			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("want error containing %q, got: %v", tc.wantSub, err)
			}
		})
	}
}

func TestResolveTargetEmptyName(t *testing.T) {
	t.Parallel()

	if _, err := resolveTarget(map[string]cty.Value{}, ""); err == nil {
		t.Fatalf("empty target name must fail")
	}
}

func TestSanitizeSendErrorMasksPassword(t *testing.T) {
	t.Parallel()

	target := Target{Name: "inbound", Protocol: "smtp", Host: "h", Port: 25, Password: "s3cr3t"}
	err := sanitizeSendError(target, errString("auth rejected for password s3cr3t"))

	if strings.Contains(err.Error(), "s3cr3t") {
		t.Fatalf("password must be scrubbed from error: %v", err)
	}
}

type errString string

func (e errString) Error() string { return string(e) }
