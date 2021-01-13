package reporter

import (
	"fmt"
)

var reporters map[string]Reporter

// Register Reporter by name
func Register(name string, reporter Reporter) error {
	if reporters == nil {
		reporters = map[string]Reporter{}
	}

	if _, ok := reporters[name]; ok {
		return fmt.Errorf("reporter %s already exists", name)
	}

	reporters[name] = reporter

	return nil
}

// GetByName return Reporter by name
func GetByName(name string) (Reporter, error) {
	if reporter, ok := reporters[name]; ok {
		return reporter, nil
	}

	return nil, fmt.Errorf("reporter %s is not exists", name)
}

// Reporter interface
type Reporter interface {
	Start() error
	ReportScenario(s *Scenario) error
	ReportCase(c *Case) error
	Stop() error
}

// Manager struct
type Manager struct {
	reporters []Reporter
}

// NewManager constructor
func NewManager(names []string) (*Manager, error) {
	rm := &Manager{}

	for _, name := range names {
		r, err := GetByName(name)
		if err != nil {
			return nil, err
		}
		rm.reporters = append(rm.reporters, r)
	}

	return rm, nil
}

// Start implements Reporter
func (rm *Manager) Start() error {
	for _, r := range rm.reporters {
		if err := r.Start(); err != nil {
			return err
		}
	}

	return nil
}

// ReportScenario implements Reporter
func (rm *Manager) ReportScenario(s *Scenario) error {
	for _, r := range rm.reporters {
		if err := r.ReportScenario(s); err != nil {
			return err
		}
	}

	return nil
}

// ReportCase implements Reporter
func (rm *Manager) ReportCase(c *Case) error {
	for _, r := range rm.reporters {
		if err := r.ReportCase(c); err != nil {
			return err
		}
	}

	return nil
}

// Stop implements Reporter
func (rm *Manager) Stop() error {
	for _, r := range rm.reporters {
		if err := r.Stop(); err != nil {
			return err
		}
	}

	return nil
}
