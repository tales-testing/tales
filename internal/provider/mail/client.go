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

// Send stages. connect / greeting / ehlo / lhlo / auth / quit failures stay
// fatal (session setup) and never surface as a structured stage here.
const (
	stageMailFrom = "mail_from"
	stageRcpt     = "rcpt"
	stageData     = "data"
	stageMessage  = "message"
	stageAccepted = "accepted"
)

// runTransaction performs MAIL FROM, per-recipient RCPT TO, and DATA on an
// already-greeted client. A protocol-level negative reply (4xx/5xx) is mapped
// into the returned Result; only transport / IO failures return an error.
func runTransaction(c *smtp.Client, envelope Envelope, raw []byte, lmtp bool) (*Result, error) {
	if err := c.Mail(envelope.From, nil); err != nil {
		if rej, ok := asRejection(stageMailFrom, "", err); ok {
			return &Result{Transaction: rej}, nil
		}

		return nil, fmt.Errorf("MAIL FROM: %w", err)
	}

	accepted, rejected, err := sendRecipients(c, envelope.Recipients)
	if err != nil {
		return nil, err
	}

	if len(accepted) == 0 {
		// Every recipient was refused at RCPT time; skip DATA. This is a
		// protocol outcome, not a provider error.
		return &Result{Rejected: rejected}, nil
	}

	wc, err := c.Data()
	if err != nil {
		if rej, ok := asRejection(stageData, "", err); ok {
			return &Result{Rejected: rejected, Transaction: rej}, nil
		}

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

		if rej, ok := asRejection(stageRcpt, rcpt, err); ok {
			rejected = append(rejected, *rej)

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
		if rej, ok := asRejection(stageMessage, "", err); ok {
			return &Result{Rejected: rejected, Transaction: rej}, nil
		}

		return nil, fmt.Errorf("DATA close: %w", err)
	}

	return &Result{Accepted: accepted, Rejected: rejected}, nil
}

// finishLMTP closes the LMTP DATA command and maps the per-recipient final
// responses: a recipient present in the LMTPDataError failed at the message
// stage, the rest were delivered.
func finishLMTP(wc *smtp.DataCommand, rcptAccepted []string, rejected []Rejection) (*Result, error) {
	_, err := wc.CloseWithLMTPResponse()

	dataRejections := smtp.LMTPDataError{}

	if err != nil && !errors.As(err, &dataRejections) {
		return nil, fmt.Errorf("DATA close: %w", err)
	}

	accepted := make([]string, 0, len(rcptAccepted))

	for _, rcpt := range rcptAccepted {
		if serr, ok := dataRejections[rcpt]; ok && serr != nil {
			rejected = append(rejected, rejectionFromSMTPError(stageMessage, rcpt, serr))

			continue
		}

		accepted = append(accepted, rcpt)
	}

	return &Result{Accepted: accepted, Rejected: rejected}, nil
}

// asRejection converts a go-smtp protocol error into a Rejection. It returns
// ok=false for transport / IO errors, which the caller must treat as fatal.
func asRejection(stage, address string, err error) (*Rejection, bool) {
	var serr *smtp.SMTPError
	if !errors.As(err, &serr) {
		return nil, false
	}

	rej := rejectionFromSMTPError(stage, address, serr)

	return &rej, true
}

// rejectionFromSMTPError maps a *smtp.SMTPError into a sanitized Rejection.
func rejectionFromSMTPError(stage, address string, serr *smtp.SMTPError) Rejection {
	return Rejection{
		Address:  address,
		Stage:    stage,
		Status:   serr.Code,
		Enhanced: formatEnhanced(serr.EnhancedCode),
		Message:  scrubMessage(serr.Message),
	}
}

// formatEnhanced renders go-smtp's [3]int enhanced status as "a.b.c", or "" when
// the reply carried no enhanced code ({0,0,0}) or it was explicitly unset
// ({-1,-1,-1}).
func formatEnhanced(code smtp.EnhancedCode) string {
	if code == (smtp.EnhancedCode{}) || code == smtp.NoEnhancedCode {
		return ""
	}

	return fmt.Sprintf("%d.%d.%d", code[0], code[1], code[2])
}
