package mail

import (
	"testing"

	smtp "github.com/emersion/go-smtp"
)

func TestFormatEnhanced(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		code smtp.EnhancedCode
		want string
	}{
		"present":      {smtp.EnhancedCode{5, 7, 1}, "5.7.1"},
		"transient":    {smtp.EnhancedCode{4, 3, 0}, "4.3.0"},
		"absent zero":  {smtp.EnhancedCode{0, 0, 0}, ""},
		"not set":      {smtp.NoEnhancedCode, ""},
		"success code": {smtp.EnhancedCode{2, 1, 5}, "2.1.5"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := formatEnhanced(tc.code); got != tc.want {
				t.Fatalf("formatEnhanced(%v): want %q got %q", tc.code, tc.want, got)
			}
		})
	}
}

func TestScrubMessageCollapsesWhitespace(t *testing.T) {
	t.Parallel()

	if got := scrubMessage("  rejected\r\n5.7.1 second line\t"); got != "rejected 5.7.1 second line" {
		t.Fatalf("scrubMessage collapsed unexpectedly: %q", got)
	}
}

func TestAsRejectionClassifiesErrors(t *testing.T) {
	t.Parallel()

	rej, ok := asRejection(stageRcpt, "a@x.test", &smtp.SMTPError{Code: 550, EnhancedCode: smtp.EnhancedCode{5, 1, 1}, Message: "user unknown"})
	if !ok {
		t.Fatalf("a *smtp.SMTPError must map to a rejection")
	}

	if rej.Stage != stageRcpt || rej.Status != 550 || rej.Enhanced != "5.1.1" || rej.Address != "a@x.test" {
		t.Fatalf("unexpected rejection: %+v", rej)
	}

	if _, ok := asRejection(stageData, "", errPlain("connection reset")); ok {
		t.Fatalf("a transport error must NOT map to a rejection (stays fatal)")
	}
}

type errPlain string

func (e errPlain) Error() string { return string(e) }
