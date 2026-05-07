package assertion

import "fmt"

// Mismatch describes one assertion mismatch.
type Mismatch struct {
	Kind    string
	Path    string
	Want    interface{}
	Got     interface{}
	Message string
}

func (m *Mismatch) Error() string {
	if m.Message != "" {
		return fmt.Sprintf("%s at %s: %s", m.Kind, m.Path, m.Message)
	}
	return fmt.Sprintf("%s at %s: want=%v got=%v", m.Kind, m.Path, m.Want, m.Got)
}
