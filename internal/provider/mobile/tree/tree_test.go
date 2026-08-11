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

func TestFindByIDNestedSameIDCollapses(t *testing.T) {
	t.Parallel()

	// A SwiftUI .topBarLeading toolbar item double-encodes one logical control
	// under a single identifier: an `other` wrapper containing the `button`,
	// both carrying the id. That is one element, not an ambiguous duplicate, so
	// FindByID returns the outermost match instead of ErrDuplicate.
	root := &ViewNode{
		ID: "navigation_bar",
		Children: []*ViewNode{
			{
				ID:      "toolbar.button",
				Type:    "other",
				Visible: true,
				Bounds:  Rect{X: 76, Y: 66, Width: 36, Height: 36},
				Children: []*ViewNode{
					{ID: "toolbar.button", Type: "button", Visible: true},
				},
			},
		},
	}

	node, ok, err := FindByID(root, "toolbar.button")
	if err != nil || !ok || node == nil {
		t.Fatalf("expected nested same-id to collapse to one match, got (%v, %v, %v)", node, ok, err)
	}

	if node.Type != "other" {
		t.Fatalf("expected the outermost (wrapper) match, got type %q", node.Type)
	}
}

func TestFindFirstByIDRoot(t *testing.T) {
	t.Parallel()

	root := sample()

	node, ok, err := FindFirstByID(root, "root")
	if err != nil || !ok || node == nil {
		t.Fatalf("expected to find root, got (%v, %v, %v)", node, ok, err)
	}
}

func TestFindFirstByIDSiblings(t *testing.T) {
	t.Parallel()

	// Sibling cells sharing one identifier — mirrors the iOS PhotosPicker
	// grid where every PXGGridLayout-Info cell carries the same id.
	root := &ViewNode{
		ID: "root",
		Children: []*ViewNode{
			{ID: "PXGGridLayout-Info", Value: "first"},
			{ID: "PXGGridLayout-Info", Value: "second"},
			{ID: "PXGGridLayout-Info", Value: "third"},
		},
	}

	node, ok, err := FindFirstByID(root, "PXGGridLayout-Info")
	if err != nil || !ok || node == nil {
		t.Fatalf("expected first sibling match, got (%v, %v, %v)", node, ok, err)
	}

	if node.Value != "first" {
		t.Fatalf("expected pre-order first match (Value=first), got %q", node.Value)
	}
}

func TestFindFirstByIDMissing(t *testing.T) {
	t.Parallel()

	node, ok, err := FindFirstByID(sample(), "does.not.exist")
	if err != nil || ok || node != nil {
		t.Fatalf("expected miss, got (%v, %v, %v)", node, ok, err)
	}
}

func TestFindFirstByIDNested(t *testing.T) {
	t.Parallel()

	// Pre-order: the outermost wrapper wins, just like FindByID's collapse,
	// but FindFirstByID stops at the first match without ever scanning the
	// inner duplicate, so the result is the same here.
	root := &ViewNode{
		ID: "navigation_bar",
		Children: []*ViewNode{
			{
				ID:   "toolbar.button",
				Type: "other",
				Children: []*ViewNode{
					{ID: "toolbar.button", Type: "button"},
				},
			},
		},
	}

	node, ok, err := FindFirstByID(root, "toolbar.button")
	if err != nil || !ok || node == nil {
		t.Fatalf("expected outermost match, got (%v, %v, %v)", node, ok, err)
	}

	if node.Type != "other" {
		t.Fatalf("expected outermost wrapper, got type %q", node.Type)
	}
}

func TestFindFirstByIDNeverErrDuplicate(t *testing.T) {
	t.Parallel()

	root := &ViewNode{
		ID: "root",
		Children: []*ViewNode{
			{ID: "dup"},
			{ID: "dup"},
		},
	}

	node, ok, err := FindFirstByID(root, "dup")
	if err != nil {
		t.Fatalf("FindFirstByID must never return an error on duplicates, got %v", err)
	}

	if !ok || node == nil {
		t.Fatalf("expected first duplicate match, got (%v, %v)", node, ok)
	}
}

func TestFindFirstByIDEmptyID(t *testing.T) {
	t.Parallel()

	node, ok, err := FindFirstByID(sample(), "")
	if err != nil || ok || node != nil {
		t.Fatalf("expected empty-id miss, got (%v, %v, %v)", node, ok, err)
	}
}

func TestFindFirstByLabelRoot(t *testing.T) {
	t.Parallel()

	root := &ViewNode{ID: "root", Label: "Welcome"}

	node, ok, err := FindFirstByLabel(root, "Welcome")
	if err != nil || !ok || node == nil {
		t.Fatalf("expected to find root by label, got (%v, %v, %v)", node, ok, err)
	}
}

func TestFindFirstByLabelSiblings(t *testing.T) {
	t.Parallel()

	// Two distinct "Done" buttons (e.g. nested system sheets) share a
	// label. FindFirstByLabel returns the pre-order first match instead
	// of erroring like the strict id path would.
	root := &ViewNode{
		ID: "root",
		Children: []*ViewNode{
			{Label: "Done", Bounds: Rect{X: 0, Y: 0, Width: 100, Height: 50}},
			{Label: "Done", Bounds: Rect{X: 0, Y: 60, Width: 100, Height: 50}},
		},
	}

	node, ok, err := FindFirstByLabel(root, "Done")
	if err != nil || !ok || node == nil {
		t.Fatalf("expected first sibling Done, got (%v, %v, %v)", node, ok, err)
	}

	if node.Bounds.Y != 0 {
		t.Fatalf("expected first sibling at Y=0, got %v", node.Bounds.Y)
	}
}

func TestFindFirstByLabelMatchesNodeWithEmptyID(t *testing.T) {
	t.Parallel()

	// iOS PHPickerViewController shape: the "Done" button has an empty
	// accessibilityIdentifier but a non-empty accessibilityLabel.
	root := &ViewNode{
		ID: "root",
		Children: []*ViewNode{
			{ID: "", Label: "Done", Visible: true, Bounds: Rect{X: 0, Y: 0, Width: 60, Height: 30}},
		},
	}

	node, ok, err := FindFirstByLabel(root, "Done")
	if err != nil || !ok || node == nil {
		t.Fatalf("expected label match on empty-id node, got (%v, %v, %v)", node, ok, err)
	}

	if node.ID != "" {
		t.Fatalf("expected matched node to keep empty id, got %q", node.ID)
	}
}

func TestFindFirstByLabelMissing(t *testing.T) {
	t.Parallel()

	node, ok, err := FindFirstByLabel(sample(), "does.not.exist")
	if err != nil || ok || node != nil {
		t.Fatalf("expected miss, got (%v, %v, %v)", node, ok, err)
	}
}

func TestFindFirstByLabelEmpty(t *testing.T) {
	t.Parallel()

	node, ok, err := FindFirstByLabel(sample(), "")
	if err != nil || ok || node != nil {
		t.Fatalf("expected empty-label miss, got (%v, %v, %v)", node, ok, err)
	}
}

func TestFindFirstByLabelNeverErrors(t *testing.T) {
	t.Parallel()

	// Three identical siblings; label resolver must never report a
	// duplicate (no ErrDuplicate analogue).
	root := &ViewNode{
		ID: "root",
		Children: []*ViewNode{
			{Label: "Cancel"},
			{Label: "Cancel"},
			{Label: "Cancel"},
		},
	}

	if _, _, err := FindFirstByLabel(root, "Cancel"); err != nil {
		t.Fatalf("FindFirstByLabel must never return an error, got %v", err)
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

func TestFindFirstByTextMatchesVisibleText(t *testing.T) {
	t.Parallel()

	root := &ViewNode{
		ID: "root",
		Children: []*ViewNode{
			{ID: "a", Text: "Cancel"},
			{ID: "b", Text: "Sign in"},
			{ID: "c", Text: "Sign in"},
		},
	}

	node, found, err := FindFirstByText(root, "Sign in")
	if err != nil || !found {
		t.Fatalf("expected a match, got found=%v err=%v", found, err)
	}

	// Pre-order first match, like the id and label locators: two
	// elements sharing visible copy is common (a list of identical
	// rows) and must not be an error.
	if node.ID != "b" {
		t.Fatalf("expected the first match in pre-order, got %q", node.ID)
	}
}

func TestFindFirstByTextFallsBackToLabel(t *testing.T) {
	t.Parallel()

	// Android reports a button's caption as text; iOS reports it as the
	// accessibility label. One locator has to reach both, or a
	// cross-platform scenario would need a per-platform spelling.
	root := &ViewNode{
		ID:       "root",
		Children: []*ViewNode{{ID: "done", Label: "Done"}},
	}

	node, found, err := FindFirstByText(root, "Done")
	if err != nil || !found {
		t.Fatalf("expected the label fallback to match, got found=%v err=%v", found, err)
	}

	if node.ID != "done" {
		t.Fatalf("matched %q", node.ID)
	}
}

func TestFindFirstByTextMissAndEmptyAreNotErrors(t *testing.T) {
	t.Parallel()

	root := &ViewNode{ID: "root", Children: []*ViewNode{{ID: "a", Text: "Cancel"}}}

	if _, found, err := FindFirstByText(root, "Absent"); found || err != nil {
		t.Fatalf("a miss must report found=false with no error, got found=%v err=%v", found, err)
	}

	if _, found, err := FindFirstByText(root, ""); found || err != nil {
		t.Fatalf("an empty locator must not match, got found=%v err=%v", found, err)
	}

	if _, found, err := FindFirstByText(nil, "Cancel"); found || err != nil {
		t.Fatalf("a nil tree must not match, got found=%v err=%v", found, err)
	}
}
