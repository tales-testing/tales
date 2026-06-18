// Package tree models a normalized UI hierarchy returned by mobile drivers.
//
// The same shape is used regardless of platform so the rest of the provider
// can reason about elements without caring about XCUITest or UIAutomator
// specifics. All values are populated by the driver client when decoding the
// driver's JSON response.
package tree

import (
	"errors"
	"fmt"
	"strings"
)

// Rect describes element bounds in screen coordinates.
type Rect struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// ViewNode is the normalized representation of one element in the UI tree.
type ViewNode struct {
	ID       string      `json:"id,omitempty"`
	Label    string      `json:"label,omitempty"`
	Text     string      `json:"text,omitempty"`
	Value    string      `json:"value,omitempty"`
	Type     string      `json:"type,omitempty"`
	Enabled  bool        `json:"enabled"`
	Visible  bool        `json:"visible"`
	Bounds   Rect        `json:"bounds"`
	Children []*ViewNode `json:"children,omitempty"`
}

// ErrDuplicate is returned by FindByID when two or more nodes share the same id.
var ErrDuplicate = errors.New("multiple elements share the same id")

// FindByID searches the tree for the node whose ID matches.
// It returns (node, true, nil) on success, (nil, false, nil) when no node is
// found, and (nil, false, ErrDuplicate) when two elements in separate subtrees
// share the id. A node and a descendant that share the id are treated as one
// logical element (the outermost match wins), since UI frameworks routinely
// double-encode a single control under one identifier.
func FindByID(root *ViewNode, id string) (*ViewNode, bool, error) {
	if root == nil || id == "" {
		return nil, false, nil
	}

	matches := make([]*ViewNode, 0, 2)

	collectByID(root, id, &matches)

	switch len(matches) {
	case 0:
		return nil, false, nil
	case 1:
		return matches[0], true, nil
	default:
		return nil, false, fmt.Errorf("%w: %q", ErrDuplicate, id)
	}
}

// FindFirstByLabel returns the first node whose Label equals label in
// pre-order depth-first traversal. It returns (nil, false, nil) on miss
// and never errors. Mirrors XCUITest's
// matching(NSPredicate(format: "label == %@", label)).firstMatch on the
// label attribute. Useful for iOS system controllers
// (PHPickerViewController / SwiftUI PhotosPicker, UIDocumentPickerViewController,
// UIActivityViewController share sheet, MFMailComposeViewController, ...)
// whose buttons expose accessibilityLabel but leave accessibilityIdentifier
// empty.
func FindFirstByLabel(root *ViewNode, label string) (*ViewNode, bool, error) {
	if root == nil || label == "" {
		return nil, false, nil
	}

	if node := firstByLabel(root, label); node != nil {
		return node, true, nil
	}

	return nil, false, nil
}

func firstByLabel(node *ViewNode, label string) *ViewNode {
	if node == nil {
		return nil
	}

	if node.Label == label {
		return node
	}

	for _, child := range node.Children {
		if found := firstByLabel(child, label); found != nil {
			return found
		}
	}

	return nil
}

// FindFirstByID returns the first node matching id in pre-order depth-first
// traversal. It returns (nil, false, nil) when no node matches and never
// reports ErrDuplicate, mirroring XCUITest's
// descendants(matching:).matching(identifier:).firstMatch semantics. Useful
// for system pickers (PhotosPicker, file importer) whose cells share an
// accessibility identifier and aren't parent-child.
func FindFirstByID(root *ViewNode, id string) (*ViewNode, bool, error) {
	if root == nil || id == "" {
		return nil, false, nil
	}

	if node := firstByID(root, id); node != nil {
		return node, true, nil
	}

	return nil, false, nil
}

func firstByID(node *ViewNode, id string) *ViewNode {
	if node == nil {
		return nil
	}

	if node.ID == id {
		return node
	}

	for _, child := range node.Children {
		if found := firstByID(child, id); found != nil {
			return found
		}
	}

	return nil
}

func collectByID(node *ViewNode, id string, out *[]*ViewNode) {
	if node == nil {
		return
	}

	if node.ID == id {
		*out = append(*out, node)

		// Stop descending into a matched node's subtree. SwiftUI (and UIKit)
		// commonly expose one logical control under a single accessibility
		// identifier on both a wrapper and its inner element — e.g. a
		// `.topBarLeading` toolbar item surfaces as an `other` container *and*
		// the `button` it wraps, both carrying the same id. Those nested
		// same-id nodes are one element, not an ambiguous duplicate, so collapse
		// them to the outermost match. Two elements in genuinely separate
		// subtrees still produce multiple matches and are reported as duplicates.
		return
	}

	for _, child := range node.Children {
		collectByID(child, id, out)
	}
}

// Center returns the screen-space center of the node's bounds.
func Center(node *ViewNode) (float64, float64) {
	if node == nil {
		return 0, 0
	}

	return node.Bounds.X + node.Bounds.Width/2, node.Bounds.Y + node.Bounds.Height/2
}

// IsVisible reports whether the driver considers the node visible.
// V1 maps approximately to (exists && isHittable) on iOS.
func IsVisible(node *ViewNode) bool {
	if node == nil {
		return false
	}

	return node.Visible
}

// Text returns the node's text falling back to its label when text is empty.
func Text(node *ViewNode) string {
	if node == nil {
		return ""
	}

	if strings.TrimSpace(node.Text) != "" {
		return node.Text
	}

	return node.Label
}

// Value returns the node's value.
func Value(node *ViewNode) string {
	if node == nil {
		return ""
	}

	return node.Value
}
