package model

// MobileActionKind identifies the kind of UI action performed inside a mobile step.
type MobileActionKind string

const (
	// MobileActionTap taps an element identified by accessibility ID.
	MobileActionTap MobileActionKind = "tap"
	// MobileActionInputText types text into an element identified by accessibility ID.
	MobileActionInputText MobileActionKind = "input_text"
	// MobileActionClearText erases the current value of an element identified by accessibility ID.
	MobileActionClearText MobileActionKind = "clear_text"
)

// MobileStep is the provider-specific payload attached to a Step when Provider == "mobile".
type MobileStep struct {
	Platform  Expression
	Target    Expression
	Launch    *MobileLaunch
	Terminate *MobileTerminate
	Actions   []MobileAction
	Expect    MobileExpect
}

// MobileLaunch describes the optional launch block of a mobile step.
type MobileLaunch struct {
	ClearState Expression
}

// MobileTerminate is the marker block requesting application termination.
type MobileTerminate struct{}

// MobileAction is one ordered UI action inside an actions block.
type MobileAction struct {
	Kind   MobileActionKind
	File   string
	Line   int
	ID     Expression
	Value  Expression
	Secure Expression
}

// MobileExpect groups visibility expectations for a mobile step.
type MobileExpect struct {
	Visible    []MobileVisibility
	NotVisible []MobileVisibility
}

// MobileVisibility describes one element visibility expectation with optional polling timeout.
type MobileVisibility struct {
	ID      Expression
	Timeout Expression
}

// HasContent reports whether the mobile step carries any operation worth executing.
func (m *MobileStep) HasContent() bool {
	if m == nil {
		return false
	}

	if m.Launch != nil || m.Terminate != nil {
		return true
	}

	if len(m.Actions) > 0 {
		return true
	}

	if len(m.Expect.Visible) > 0 || len(m.Expect.NotVisible) > 0 {
		return true
	}

	return false
}
