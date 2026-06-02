package mail

import (
	"testing"

	"github.com/tales-testing/tales/internal/provider"
	"github.com/zclconf/go-cty/cty"
)

func TestToSendResponseAccepted(t *testing.T) {
	t.Parallel()

	out := toSendResponse("<m@tales.local>", &Result{Accepted: []string{"a@x.test"}}, "smtp")
	json := out["json"]

	if !json.GetAttr("accepted").True() || json.GetAttr("rejected").True() {
		t.Fatalf("accepted result must be accepted=true rejected=false")
	}

	if got := json.GetAttr("stage").AsString(); got != stageAccepted {
		t.Fatalf("accepted stage: want %q got %q", stageAccepted, got)
	}

	if !json.GetAttr("status_code").IsNull() {
		t.Fatalf("accepted status_code must be null")
	}
}

func TestToSendResponseTransactionRejection(t *testing.T) {
	t.Parallel()

	result := &Result{Transaction: &Rejection{Stage: stageMailFrom, Status: 550, Enhanced: "5.7.1", Message: "sender domain rejected"}}
	json := toSendResponse("<m@tales.local>", result, "smtp")["json"]

	if json.GetAttr("accepted").True() || !json.GetAttr("rejected").True() {
		t.Fatalf("transaction rejection must be accepted=false rejected=true")
	}

	if got := json.GetAttr("stage").AsString(); got != stageMailFrom {
		t.Fatalf("stage: want mail_from got %q", got)
	}

	if json.GetAttr("status_code").AsBigFloat().Cmp(cty.NumberIntVal(550).AsBigFloat()) != 0 {
		t.Fatalf("status_code should be 550")
	}

	if json.GetAttr("enhanced_status_code").AsString() != "5.7.1" {
		t.Fatalf("enhanced_status_code should be 5.7.1")
	}
}

func TestToSendResponseTopLevelFromFirstRecipient(t *testing.T) {
	t.Parallel()

	result := &Result{Rejected: []Rejection{
		{Address: "bad@x.test", Stage: stageRcpt, Status: 551, Enhanced: "5.1.1", Message: "user unknown"},
	}}
	json := toSendResponse("<m@tales.local>", result, "smtp")["json"]

	if got := json.GetAttr("stage").AsString(); got != stageRcpt {
		t.Fatalf("top-level stage should mirror first recipient rejection, got %q", got)
	}

	if json.GetAttr("status_code").AsBigFloat().Cmp(cty.NumberIntVal(551).AsBigFloat()) != 0 {
		t.Fatalf("top-level status_code should mirror first recipient rejection")
	}
}

func TestBuildRequestMetaFiltersReservedHeaders(t *testing.T) {
	t.Parallel()

	exec := &provider.MailExecution{
		From:    "sender@example.com",
		To:      []string{"a@example.test"},
		Subject: "real subject",
		Headers: map[string]string{
			"X-Test-ID":  "abc",
			"Subject":    "ignored",   // reserved: generated from the explicit field
			"From":       "spoof@x",   // reserved
			"Message-ID": "<spoof@x>", // reserved
		},
		MessageID: "<m@tales.local>",
	}

	meta := buildRequestMeta(Target{Name: "inbound", Protocol: "smtp"}, exec, messageSpec{})

	headers := meta["headers"].AsValueMap()

	if _, ok := headers["X-Test-ID"]; !ok {
		t.Errorf("custom header X-Test-ID must be reported")
	}

	for _, reserved := range []string{"Subject", "From", "Message-ID"} {
		if _, ok := headers[reserved]; ok {
			t.Errorf("reserved header %q must not appear in report metadata (it is not sent verbatim)", reserved)
		}
	}
}

func TestBuildRequestMetaOmitsBody(t *testing.T) {
	t.Parallel()

	exec := &provider.MailExecution{
		From: "sender@example.com",
		To:   []string{"a@example.test"},
		Text: "super secret body",
		HTML: "<p>secret</p>",
	}

	meta := buildRequestMeta(Target{Name: "inbound", Protocol: "smtp"}, exec, messageSpec{})

	for _, banned := range []string{"body", "text", "html", "raw", "raw_body"} {
		if _, ok := meta[banned]; ok {
			t.Errorf("request metadata must not carry a %q key", banned)
		}
	}
}

func TestNonReservedHeadersNilSafe(t *testing.T) {
	t.Parallel()

	if got := nonReservedHeaders(nil); len(got) != 0 {
		t.Fatalf("nil headers must stay empty, got %v", got)
	}
}
