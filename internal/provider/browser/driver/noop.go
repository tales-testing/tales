package driver

import "context"

// NoopDriver is an embedding base for test fakes. Every method returns a
// zero value / nil error so individual tests override only the methods they
// exercise.
type NoopDriver struct{}

// Goto satisfies Driver.
func (NoopDriver) Goto(_ context.Context, _ string) error { return nil }

// Click satisfies Driver.
func (NoopDriver) Click(_ context.Context, _ string) error { return nil }

// Fill satisfies Driver.
func (NoopDriver) Fill(_ context.Context, _, _ string) error { return nil }

// Clear satisfies Driver.
func (NoopDriver) Clear(_ context.Context, _ string) error { return nil }

// Press satisfies Driver.
func (NoopDriver) Press(_ context.Context, _, _ string) error { return nil }

// Submit satisfies Driver.
func (NoopDriver) Submit(_ context.Context, _ string) error { return nil }

// Hover satisfies Driver.
func (NoopDriver) Hover(_ context.Context, _ string) error { return nil }

// Check satisfies Driver.
func (NoopDriver) Check(_ context.Context, _ string) error { return nil }

// Uncheck satisfies Driver.
func (NoopDriver) Uncheck(_ context.Context, _ string) error { return nil }

// SelectOption satisfies Driver.
func (NoopDriver) SelectOption(_ context.Context, _, _ string) error { return nil }

// ScrollIntoView satisfies Driver.
func (NoopDriver) ScrollIntoView(_ context.Context, _ string) error { return nil }

// ScrollBy satisfies Driver.
func (NoopDriver) ScrollBy(_ context.Context, _, _ int) error { return nil }

// Reload satisfies Driver.
func (NoopDriver) Reload(_ context.Context) error { return nil }

// Back satisfies Driver.
func (NoopDriver) Back(_ context.Context) error { return nil }

// Forward satisfies Driver.
func (NoopDriver) Forward(_ context.Context) error { return nil }

// WaitVisible satisfies Driver.
func (NoopDriver) WaitVisible(_ context.Context, _ string) error { return nil }

// WaitNotVisible satisfies Driver.
func (NoopDriver) WaitNotVisible(_ context.Context, _ string) error { return nil }

// Visible satisfies Driver.
func (NoopDriver) Visible(_ context.Context, _ string) (bool, error) { return false, nil }

// Text satisfies Driver.
func (NoopDriver) Text(_ context.Context, _ string) (string, error) { return "", nil }

// InputValue satisfies Driver.
func (NoopDriver) InputValue(_ context.Context, _ string) (string, error) { return "", nil }

// Attribute satisfies Driver.
func (NoopDriver) Attribute(_ context.Context, _, _ string) (string, bool, error) {
	return "", false, nil
}

// URL satisfies Driver.
func (NoopDriver) URL(_ context.Context) (string, error) { return "", nil }

// Title satisfies Driver.
func (NoopDriver) Title(_ context.Context) (string, error) { return "", nil }

// OuterHTML satisfies Driver.
func (NoopDriver) OuterHTML(_ context.Context, _ string) (string, error) { return "", nil }

// Screenshot satisfies Driver.
func (NoopDriver) Screenshot(_ context.Context) ([]byte, error) { return nil, nil }

// Performance satisfies Driver.
func (NoopDriver) Performance(_ context.Context) (*PerformanceMetrics, error) { return nil, nil }

// Close satisfies Driver.
func (NoopDriver) Close(_ context.Context) error { return nil }
