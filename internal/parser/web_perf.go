package parser

import (
	"sort"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/tales-testing/tales/internal/model"
)

// Canonical web performance metric names exposed by the driver layer.
// The alias table below maps friendly DSL names to these, and the
// provider's matcher dispatches on the same values.
const (
	webPerfFCP  = "fcp_ms"
	webPerfLCP  = "lcp_ms"
	webPerfCLS  = "cls"
	webPerfLoad = "load_event_ms"
	webPerfDCL  = "dom_content_loaded_ms"
	webPerfResC = "resources_count"
	webPerfXfer = "transfer_size_bytes"
	webPerfEnc  = "encoded_body_size_bytes"
	webPerfDec  = "decoded_body_size_bytes"
)

// webPerfAliases maps the friendly DSL attribute names users write
// inside `expect { web_perf { ... } }` to the canonical metric names.
// New aliases extend this table and the docs together.
var webPerfAliases = map[string]string{
	"fcp":                     webPerfFCP,
	"fcp_ms":                  webPerfFCP,
	"lcp":                     webPerfLCP,
	"lcp_ms":                  webPerfLCP,
	"cls":                     webPerfCLS,
	"load":                    webPerfLoad,
	"load_event":              webPerfLoad,
	"load_event_ms":           webPerfLoad,
	"dom_ready":               webPerfDCL,
	"dom_content_loaded":      webPerfDCL,
	"dom_content_loaded_ms":   webPerfDCL,
	"resources_count":         webPerfResC,
	"transfer_size":           webPerfXfer,
	"transfer_size_bytes":     webPerfXfer,
	"encoded_body_size":       webPerfEnc,
	"encoded_body_size_bytes": webPerfEnc,
	"decoded_body_size":       webPerfDec,
	"decoded_body_size_bytes": webPerfDec,
}

// decodeWebPerfBlocks walks every web_perf block on the expect, reads
// the dynamic attribute list via hclsyntax to preserve declaration
// order, and produces model.BrowserWebPerfExpectation entries. Unknown
// aliases are rejected at parse time so authoring mistakes surface
// before any browser session is started.
func decodeWebPerfBlocks(path string, blocks []*webPerfBlock, out *model.BrowserExpect) hcl.Diagnostics {
	var diags hcl.Diagnostics

	for _, block := range blocks {
		if block == nil {
			continue
		}

		body, ok := block.Body.(*hclsyntax.Body)
		if !ok {
			continue
		}

		attrs := sortAttrsByByteOffset(body)

		for _, attr := range attrs {
			canonical, known := webPerfAliases[attr.Name]
			if !known {
				rng := attr.NameRange

				diags = append(diags, &hcl.Diagnostic{
					Severity: hcl.DiagError,
					Summary:  "unknown web_perf metric",
					Detail:   "supported metrics: fcp, lcp, cls, load, dom_content_loaded, resources_count, transfer_size, encoded_body_size, decoded_body_size.",
					Subject:  &rng,
				})

				continue
			}

			out.WebPerf = append(out.WebPerf, model.BrowserWebPerfExpectation{
				Metric:   canonical,
				Expected: expr(path, attr.Expr),
			})
		}
	}

	return diags
}

// sortAttrsByByteOffset returns the body's attributes sorted by the
// byte offset of their declaration so the model preserves source
// order even when go-maps iterate non-deterministically.
func sortAttrsByByteOffset(body *hclsyntax.Body) []*hclsyntax.Attribute {
	attrs := make([]*hclsyntax.Attribute, 0, len(body.Attributes))
	for _, attr := range body.Attributes {
		attrs = append(attrs, attr)
	}

	sort.Slice(attrs, func(i, j int) bool {
		return attrs[i].Range().Start.Byte < attrs[j].Range().Start.Byte
	})

	return attrs
}
