package runtime

import (
	"fmt"
	"strings"

	"github.com/andybalholm/cascadia"
	"golang.org/x/net/html"
)

// findFirstNode parses dom and returns the first node matching selector.
// Returns an error when the snapshot is empty or the selector matches no
// element so capture diagnostics surface the problem cleanly.
func findFirstNode(dom, selector string) (*html.Node, error) {
	if strings.TrimSpace(dom) == "" {
		return nil, fmt.Errorf("dom snapshot is empty")
	}

	root, err := html.Parse(strings.NewReader(dom))
	if err != nil {
		return nil, fmt.Errorf("parse dom: %w", err)
	}

	sel, err := cascadia.Parse(selector)
	if err != nil {
		return nil, fmt.Errorf("invalid selector %q: %w", selector, err)
	}

	match := cascadia.Query(root, sel)
	if match == nil {
		return nil, fmt.Errorf("selector %q matched no element", selector)
	}

	return match, nil
}

// nodeText collects the rendered text content of node, mirroring DOM
// textContent (concatenation of every descendant text node, no extra
// whitespace).
func nodeText(node *html.Node) string {
	if node == nil {
		return ""
	}

	var b strings.Builder

	var walk func(*html.Node)

	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}

	walk(node)

	return strings.TrimSpace(b.String())
}

// nodeAttr returns the named attribute on node, or ("", false) when it is
// absent.
func nodeAttr(node *html.Node, name string) (string, bool) {
	if node == nil {
		return "", false
	}

	for _, attr := range node.Attr {
		if attr.Key == name {
			return attr.Val, true
		}
	}

	return "", false
}
