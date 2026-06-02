package mail

import (
	"errors"
	"fmt"

	smtp "github.com/emersion/go-smtp"
)

// heloName is the fixed client identity sent in EHLO / LHLO. It is deliberately
// constant (never os.Hostname) so it is deterministic and never leaks the test
// machine name into reports or errors.
const heloName = "tales.local"

// runTransaction performs MAIL FROM, per-recipient RCPT TO, and DATA on an
// already-greeted client. lmtp selects the per-recipient final-response read.
func runTransaction(c *smtp.Client, envelope Envelope, raw []byte, lmtp bool) (*Result, error) {
	if err := c.Mail(envelope.From, nil); err != nil {
		return nil, fmt.Errorf("MAIL FROM: %w", err)
	}

	accepted, rejected, err := sendRecipients(c, envelope.Recipients)
	if err != nil {
		return nil, err
	}

	if len(accepted) == 0 {
		// Every recipient was refused at RCPT time; skip DATA and let the
		// provider turn this into a step failure.
		return &Result{Rejected: rejected}, nil
	}

	wc, err := c.Data()
	if err != nil {
		return nil, fmt.Errorf("DATA: %w", err)
	}

	if _, err := wc.Write(raw); err != nil {
		_ = wc.Close()

		return nil, fmt.Errorf("write DATA: %w", err)
	}

	if lmtp {
		return finishLMTP(wc, accepted, rejected)
	}

	return finishSMTP(wc, accepted, rejected)
}

// sendRecipients issues one RCPT TO per recipient and classifies each as
// accepted or rejected. A non-protocol (I/O) error aborts the whole send.
func sendRecipients(c *smtp.Client, recipients []string) ([]string, []Rejection, error) {
	accepted := make([]string, 0, len(recipients))

	var rejected []Rejection

	for _, rcpt := range recipients {
		err := c.Rcpt(rcpt, nil)
		if err == nil {
			accepted = append(accepted, rcpt)

			continue
		}

		var serr *smtp.SMTPError
		if errors.As(err, &serr) {
			rejected = append(rejected, Rejection{Address: rcpt, Status: serr.Code, Message: serr.Message})

			continue
		}

		return nil, nil, fmt.Errorf("RCPT TO: %w", err)
	}

	return accepted, rejected, nil
}

// finishSMTP closes the SMTP DATA command; the single final response applies to
// the whole message.
func finishSMTP(wc *smtp.DataCommand, accepted []string, rejected []Rejection) (*Result, error) {
	if err := wc.Close(); err != nil {
		return nil, fmt.Errorf("DATA close: %w", err)
	}

	return &Result{Accepted: accepted, Rejected: rejected}, nil
}

// finishLMTP closes the LMTP DATA command and maps the per-recipient final
// responses: a recipient present in the LMTPDataError failed at DATA, the rest
// were delivered.
func finishLMTP(wc *smtp.DataCommand, rcptAccepted []string, rejected []Rejection) (*Result, error) {
	_, err := wc.CloseWithLMTPResponse()

	dataRejections := smtp.LMTPDataError{}

	if err != nil && !errors.As(err, &dataRejections) {
		return nil, fmt.Errorf("DATA close: %w", err)
	}

	accepted := make([]string, 0, len(rcptAccepted))

	for _, rcpt := range rcptAccepted {
		if serr, ok := dataRejections[rcpt]; ok && serr != nil {
			rejected = append(rejected, Rejection{Address: rcpt, Status: serr.Code, Message: serr.Message})

			continue
		}

		accepted = append(accepted, rcpt)
	}

	return &Result{Accepted: accepted, Rejected: rejected}, nil
}
