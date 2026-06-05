package parser

import (
	"fmt"

	"github.com/hashicorp/hcl/v2"
	"github.com/tales-testing/tales/internal/model"
)

// httpProviderType is the provider label that produces an HTTP response, the
// only step kind whose body a save block can persist.
const httpProviderType = "http"

// decodeSaveBlock decodes the optional save { body = "<path>" } block. The
// block writes the raw HTTP response body to the scenario workspace, so it is
// rejected on any non-http provider. body is required when the block is
// present; the path itself is resolved at runtime against scenario.workdir.
func decodeSaveBlock(path string, rs stepBlock, step *model.Step) hcl.Diagnostics {
	if rs.Save == nil {
		return nil
	}

	diags := make(hcl.Diagnostics, 0)

	if rs.Provider != "" && rs.Provider != httpProviderType {
		diags = append(diags, diagError(
			"Unexpected save block",
			fmt.Sprintf("save { } persists the HTTP response body and is only valid on http steps; step %q uses provider %q.", step.Name, rs.Provider),
			nil,
		))
	}

	if !exprIsSet(rs.Save.Body) {
		diags = append(diags, diagError(
			"Missing save body",
			"save block must declare body = \"<path>\".",
			nil,
		))
	}

	step.Save = &model.SaveBlock{Body: optionalExpr(path, rs.Save.Body)}

	return diags
}
