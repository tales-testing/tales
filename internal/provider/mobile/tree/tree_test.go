package tree

import (
	"errors"
	"testing"
)

func sample() *ViewNode {
	return &ViewNode{
		ID:      "root",
		Type:    "application",
		Enabled: true,
		Visible: true,
		Bounds:  Rect{X: 0, Y: 0, Width: 390, Height: 844},
		Children: []*ViewNode{
			{
				ID:      "welcome.register",
				Type:    "button",
				Label:   "Create account",
				Enabled: true,
				Visible: true,
				Bounds:  Rect{X: 20, Y: 100, Width: 100, Height: 40},
			},
			{
				ID:      "register.form",
				Type:    "form",
				Enabled: true,
				Visible: true,
				Children: []*ViewNode{
					{
						ID:      "register.email",
						Type:    "text_field",
						Value:   "user@example.com",
						Enabled: true,
						Visible: true,
						Bounds:  Rect{X: 0, Y: 200, Width: 200, Height: 20},
					},
					{
						ID:      "register.title",
						Type:    "label",
						Label:   "Register",
						Enabled: true,
						Visible: true,
					},
				},
			},
		},
	}
}

func TestFindByIDRoot(t *testing.T) {
	t.Parallel()

	root := sample()

	node, ok, err := FindByID(root, "root")
	if err != nil || !ok || node == nil {
		t.Fatalf("expected to find root, got (%v, %v, %v)", node, ok, err)
	}
}

func TestFindByIDNested(t *testing.T) {
	t.Parallel()

	root := sample()

	node, ok, err := FindByID(root, "register.email")
	if err != nil || !ok || node == nil {
		t.Fatalf("expected to find nested node, got (%v, %v, %v)", node, ok, err)
	}

	if node.Value != "user@example.com" {
		t.Fatalf("expected nested email value, got %q", node.Value)
	}
}

func TestFindByIDMissing(t *testing.T) {
	t.Parallel()

	node, ok, err := FindByID(sample(), "does.not.exist")
	if err != nil || ok || node != nil {
		t.Fatalf("expected miss, got (%v, %v, %v)", node, ok, err)
	}
}

func TestFindByIDDuplicate(t *testing.T) {
	t.Parallel()

	root := &ViewNode{
		ID: "root",
		Children: []*ViewNode{
			{ID: "dup"},
			{ID: "dup"},
		},
	}

	_, _, err := FindByID(root, "dup")
	if !errors.Is(err, ErrDuplicate) {
		t.Fatalf("expected ErrDuplicate, got %v", err)
	}
}

func TestCenter(t *testing.T) {
	t.Parallel()

	node := &ViewNode{Bounds: Rect{X: 10, Y: 20, Width: 100, Height: 40}}

	x, y := Center(node)
	if x != 60 || y != 40 {
		t.Fatalf("expected center (60, 40), got (%v, %v)", x, y)
	}
}

func TestCenterNil(t *testing.T) {
	t.Parallel()

	x, y := Center(nil)
	if x != 0 || y != 0 {
		t.Fatalf("expected (0,0) for nil, got (%v, %v)", x, y)
	}
}

func TestIsVisibleNil(t *testing.T) {
	t.Parallel()

	if IsVisible(nil) {
		t.Fatal("nil node must not be visible")
	}
}

func TestTextFallbackToLabel(t *testing.T) {
	t.Parallel()

	node := &ViewNode{Label: "fallback"}
	if Text(node) != "fallback" {
		t.Fatalf("expected fallback, got %q", Text(node))
	}
}

func TestTextWhitespaceFallsBackToLabel(t *testing.T) {
	t.Parallel()

	node := &ViewNode{Text: "   ", Label: "label-value"}
	if Text(node) != "label-value" {
		t.Fatalf("expected label-value, got %q", Text(node))
	}
}

func TestTextPrefersTextOverLabel(t *testing.T) {
	t.Parallel()

	node := &ViewNode{Text: "primary", Label: "fallback"}
	if Text(node) != "primary" {
		t.Fatalf("expected primary, got %q", Text(node))
	}
}

func TestValue(t *testing.T) {
	t.Parallel()

	node := &ViewNode{Value: "v"}
	if Value(node) != "v" {
		t.Fatalf("expected v, got %q", Value(node))
	}

	if Value(nil) != "" {
		t.Fatal("expected empty value for nil")
	}
}
