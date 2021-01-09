package configs

import (
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"
)

// HTTPCase represents a `case "http"`
type HTTPCase struct {
	Name     string
	Request  *HTTPRequest  `hcl:"request,block"`
	Response *HTTPResponse `hcl:"response,block"`
	result   Result

	DeclRange hcl.Range
	TypeRange hcl.Range
}

// Execute case
func (c *HTTPCase) Execute() {

}

// HTTPRequest represents a `request` block in `case "http"`
type HTTPRequest struct {
	URL     string
	Method  string
	Headers map[string]string
	Body    string
}

// HTTPResponse represents a `request` block in `case "http"`
type HTTPResponse struct {
	StatusCode int
	Headers    map[string]string
	Body       string
}

func decodeScenarioHTTPCaseBlock(block *hcl.Block) (*HTTPCase, hcl.Diagnostics) {
	var diags hcl.Diagnostics
	c := &HTTPCase{
		Name: block.Labels[1],
	}

	content, moreDiags := block.Body.Content(scenarioCaseHTTPBlockSchema)
	diags = append(diags, moreDiags...)

	for _, block := range content.Blocks {
		switch block.Type {
		case "request":
			req, reqDiags := decodeScenarioHTTPRequestBlock(block)
			diags = append(diags, reqDiags...)
			c.Request = req

		case "response":
			resp, respDiags := decodeScenarioHTTPResponseBlock(block)
			diags = append(diags, respDiags...)
			c.Response = resp
		}
	}

	return c, diags
}

func decodeScenarioHTTPRequestBlock(block *hcl.Block) (*HTTPRequest, hcl.Diagnostics) {
	var diags hcl.Diagnostics
	r := &HTTPRequest{}

	content, moreDiags := block.Body.Content(scenarioCaseRequestBlockSchema)
	diags = append(diags, moreDiags...)

	if attr, exists := content.Attributes["url"]; exists {
		valDiags := gohcl.DecodeExpression(attr.Expr, nil, &r.URL)
		diags = append(diags, valDiags...)
	}

	if attr, exists := content.Attributes["method"]; exists {
		valDiags := gohcl.DecodeExpression(attr.Expr, nil, &r.Method)
		diags = append(diags, valDiags...)
	}

	if attr, exists := content.Attributes["headers"]; exists {
		valDiags := gohcl.DecodeExpression(attr.Expr, nil, &r.Headers)
		diags = append(diags, valDiags...)
	}

	if attr, exists := content.Attributes["body"]; exists {
		valDiags := gohcl.DecodeExpression(attr.Expr, nil, &r.Body)
		diags = append(diags, valDiags...)
	}

	return r, diags
}

func decodeScenarioHTTPResponseBlock(block *hcl.Block) (*HTTPResponse, hcl.Diagnostics) {
	var diags hcl.Diagnostics
	r := &HTTPResponse{}

	content, moreDiags := block.Body.Content(scenarioCaseResponseBlockSchema)
	diags = append(diags, moreDiags...)

	if attr, exists := content.Attributes["status_code"]; exists {
		valDiags := gohcl.DecodeExpression(attr.Expr, nil, &r.StatusCode)
		diags = append(diags, valDiags...)
	}

	if attr, exists := content.Attributes["headers"]; exists {
		valDiags := gohcl.DecodeExpression(attr.Expr, nil, &r.Headers)
		diags = append(diags, valDiags...)
	}

	if attr, exists := content.Attributes["body"]; exists {
		valDiags := gohcl.DecodeExpression(attr.Expr, nil, &r.Body)
		diags = append(diags, valDiags...)
	}

	return r, diags
}

var scenarioCaseHTTPBlockSchema = &hcl.BodySchema{
	Blocks: []hcl.BlockHeaderSchema{
		{Type: "request"},
		{Type: "response"},
	},
}

var scenarioCaseRequestBlockSchema = &hcl.BodySchema{
	Attributes: []hcl.AttributeSchema{
		{
			Name: "url",
		},
		{
			Name: "method",
		},
		{
			Name: "headers",
		},
		{
			Name: "body",
		},
	},
}

var scenarioCaseResponseBlockSchema = &hcl.BodySchema{
	Attributes: []hcl.AttributeSchema{
		{
			Name: "status_code",
		},
		{
			Name: "headers",
		},
		{
			Name: "body",
		},
	},
}
