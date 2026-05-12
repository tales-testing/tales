package visual

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"strings"

	"github.com/hyperxlab/tales/internal/report"
)

// templates carries the embedded HTML/CSS/JS shipped inside the tales
// binary. The visual report is a single self-contained file: at write time
// the CSS and JS are inlined inside the HTML, and the model is JSON-encoded
// into a data island that the JS reads at boot.
//
//go:embed templates/visual.html templates/visual.css templates/visual.js
var templates embed.FS

// Write renders the visual HTML report for result at path. The parent
// directory is created with 0o755 if missing. Screenshot and hierarchy
// artifact paths in the JSON payload are made relative to path's directory
// when possible so the report stays portable.
func Write(path string, result *report.SuiteResult) error {
	model := Build(result, path)

	rendered, err := renderHTML(model)
	if err != nil {
		return fmt.Errorf("render visual report: %w", err)
	}

	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create report dir %q: %w", dir, err)
		}
	}

	if err := os.WriteFile(path, rendered, 0o600); err != nil {
		return fmt.Errorf("write %q: %w", path, err)
	}

	return nil
}

func renderHTML(model Report) ([]byte, error) {
	htmlBytes, err := templates.ReadFile("templates/visual.html")
	if err != nil {
		return nil, fmt.Errorf("read visual.html: %w", err)
	}

	cssBytes, err := templates.ReadFile("templates/visual.css")
	if err != nil {
		return nil, fmt.Errorf("read visual.css: %w", err)
	}

	jsBytes, err := templates.ReadFile("templates/visual.js")
	if err != nil {
		return nil, fmt.Errorf("read visual.js: %w", err)
	}

	payload, err := safeJSON(model)
	if err != nil {
		return nil, err
	}

	tmpl, err := template.New("visual").Funcs(template.FuncMap{
		// The CSS / JS / JSON payload inputs are trusted: they come from
		// the embed.FS shipped with the binary and from our own JSON
		// encoder, never from user-supplied data. The template.CSS /
		// template.JS conversions intentionally bypass auto-escaping so
		// the assets render verbatim.
		"css": func() template.CSS {
			//nolint:gosec // G203: trusted embedded asset, not user input
			return template.CSS(cssBytes)
		},
		"js": func() template.JS {
			//nolint:gosec // G203: trusted embedded asset, not user input
			return template.JS(jsBytes)
		},
		"jsonPayload": func() template.JS {
			//nolint:gosec // G203: JSON payload from our own encoder, defused for </script
			return template.JS(payload)
		},
	}).Parse(string(htmlBytes))
	if err != nil {
		return nil, fmt.Errorf("parse visual.html: %w", err)
	}

	data := struct {
		Title string
	}{
		Title: model.Title,
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("execute visual template: %w", err)
	}

	return buf.Bytes(), nil
}

// safeJSON encodes the model and defuses the embedded-script-tag attack by
// escaping any literal "</script" sequence the JSON encoder may have left
// alone. Returns the byte slice ready to be inlined inside
// <script type="application/json">...</script>.
func safeJSON(model Report) ([]byte, error) {
	raw, err := json.Marshal(model)
	if err != nil {
		return nil, fmt.Errorf("encode visual report: %w", err)
	}

	defused := strings.ReplaceAll(string(raw), "</script", "<\\/script")

	return []byte(defused), nil
}
