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

// FindByID searches the tree for the first node whose ID matches.
// It returns (node, true, nil) on success, (nil, false, nil) when no node is
// found, and (nil, false, ErrDuplicate) when more than one match exists.
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

func collectByID(node *ViewNode, id string, out *[]*ViewNode) {
	if node == nil {
		return
	}

	if node.ID == id {
		*out = append(*out, node)
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
