package report

import (
	"fmt"
	"io"
)

// jsonResponseKey is the response map key under which providers expose their
// assertable payload.
const jsonResponseKey = "json"

// printProviderSummary dispatches to the per-provider console recap (load,
// mail). Providers without a recap are a no-op. Extracted from printStep to
// keep its cyclomatic complexity within budget.
func printProviderSummary(out io.Writer, step *StepResult) error {
	switch step.Provider {
	case "load":
		return printLoadSummary(out, step)
	case "mail":
		return printMailSummary(out, step)
	default:
		return nil
	}
}

// printMailSummary writes a concise recap after a mail step header line:
// protocol, Message-ID, and the accepted / rejected recipient counts (with
// per-recipient detail for rejections). It degrades to a no-op when the
// response keys are missing. No message body or credentials are ever printed.
func printMailSummary(out io.Writer, step *StepResult) error {
	if step == nil || step.Response == nil {
		return nil
	}

	payload, ok := step.Response[jsonResponseKey].(map[string]interface{})
	if !ok {
		return nil
	}

	protocol, _ := payload["protocol"].(string)
	messageID, _ := payload["message_id"].(string)

	if _, err := fmt.Fprintf(out, "    protocol: %s\n", protocol); err != nil {
		return fmt.Errorf("print mail protocol: %w", err)
	}

	if messageID != "" {
		if _, err := fmt.Fprintf(out, "    message-id: %s\n", messageID); err != nil {
			return fmt.Errorf("print mail message-id: %w", err)
		}
	}

	return printMailRecipients(out, payload)
}

func printMailRecipients(out io.Writer, payload map[string]interface{}) error {
	recipients, ok := payload["recipients"].(map[string]interface{})
	if !ok {
		return nil
	}

	accepted, _ := recipients["accepted"].([]interface{})
	rejected, _ := recipients["rejected"].([]interface{})

	if _, err := fmt.Fprintf(out, "    recipients: %d accepted, %d rejected\n", len(accepted), len(rejected)); err != nil {
		return fmt.Errorf("print mail recipients: %w", err)
	}

	for _, raw := range rejected {
		entry, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}

		address, _ := entry["address"].(string)
		message, _ := entry["message"].(string)
		status, _ := numericField(entry, "status")

		if _, err := fmt.Fprintf(out, "      rejected %s: %s %s\n", address, formatInt(status), message); err != nil {
			return fmt.Errorf("print mail rejection: %w", err)
		}
	}

	return nil
}
