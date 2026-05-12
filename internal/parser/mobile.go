package parser

import (
	"fmt"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/hyperxlab/tales/internal/model"
)

// mobileProviderType is the provider label that triggers mobile step decoding.
const mobileProviderType = "mobile"

// supportedMobilePlatform is the only platform accepted by V1.
const supportedMobilePlatform = "ios"

const mobileTimeoutAttr = "timeout"
const mobileIntervalAttr = "interval"

// decodeMobileStep builds a model.MobileStep from a parsed step block when any
// mobile-specific attribute or block is present. It returns nil when the step
// is not a mobile step.
func decodeMobileStep(path string, rs stepBlock) (*model.MobileStep, hcl.Diagnostics) {
	if !looksLikeMobileStep(rs) {
		return nil, nil
	}

	diags := make(hcl.Diagnostics, 0)
	ms := &model.MobileStep{}

	if exprIsSet(rs.Platform) {
		ms.Platform = expr(path, rs.Platform)
	}

	if exprIsSet(rs.Target) {
		ms.Target = expr(path, rs.Target)
	}

	platformDiags := validateMobilePlatform(rs)
	diags = append(diags, platformDiags...)

	if !exprIsSet(rs.Target) {
		diags = append(diags, diagError("Missing mobile target", "mobile step must declare target = \"<name>\" pointing at a config.mobile.targets entry.", nil))
	}

	if rs.Launch != nil {
		ms.Launch = &model.MobileLaunch{ClearState: expr(path, rs.Launch.ClearState)}
	}

	if rs.Terminate != nil {
		ms.Terminate = &model.MobileTerminate{}
	}

	if rs.Actions != nil {
		actions, aDiags := decodeMobileActions(path, rs.Actions.Body)
		diags = append(diags, aDiags...)
		ms.Actions = actions
	}

	expectBody := rs.Expect
	if expectBody == nil {
		expectBody = rs.Response
	}

	if expectBody != nil {
		for _, v := range expectBody.Visible {
			ms.Expect.Visible = append(ms.Expect.Visible, mobileVisibilityFromBlock(path, v))
		}

		for _, v := range expectBody.NotVisible {
			ms.Expect.NotVisible = append(ms.Expect.NotVisible, mobileVisibilityFromBlock(path, v))
		}

		for _, v := range expectBody.Text {
			ms.Expect.Text = append(ms.Expect.Text, mobileValueExpectationFromBlock(path, v))
		}

		for _, v := range expectBody.Value {
			ms.Expect.Value = append(ms.Expect.Value, mobileValueExpectationFromBlock(path, v))
		}

		for _, v := range expectBody.Enabled {
			ms.Expect.Enabled = append(ms.Expect.Enabled, mobileStateExpectationFromBlock(path, v))
		}

		for _, v := range expectBody.Disabled {
			ms.Expect.Disabled = append(ms.Expect.Disabled, mobileStateExpectationFromBlock(path, v))
		}
	}

	return ms, diags
}

func looksLikeMobileStep(rs stepBlock) bool {
	if rs.Provider == mobileProviderType {
		return true
	}

	if exprIsSet(rs.Platform) || exprIsSet(rs.Target) {
		return true
	}

	if rs.Launch != nil || rs.Terminate != nil || rs.Actions != nil {
		return true
	}

	if rs.Expect != nil && mobileExpectHasContent(rs.Expect) {
		return true
	}

	if rs.Response != nil && mobileExpectHasContent(rs.Response) {
		return true
	}

	return false
}

func mobileExpectHasContent(expect *expectBlock) bool {
	return len(expect.Visible) > 0 || len(expect.NotVisible) > 0 ||
		len(expect.Text) > 0 || len(expect.Value) > 0 ||
		len(expect.Enabled) > 0 || len(expect.Disabled) > 0
}

// exprIsSet reports whether the user actually provided this optional HCL
// expression. gohcl wraps missing optional hcl.Expression fields with a
// zero-range placeholder, so the canonical nil check is unreliable.
func exprIsSet(e hcl.Expression) bool {
	if e == nil {
		return false
	}

	rng := e.Range()

	return rng.Start != rng.End
}

func validateMobilePlatform(rs stepBlock) hcl.Diagnostics {
	diags := make(hcl.Diagnostics, 0)

	if !exprIsSet(rs.Platform) {
		diags = append(diags, diagError("Missing mobile platform", "mobile step must declare platform = \"ios\".", nil))

		return diags
	}

	value, valueDiags := rs.Platform.Value(nil)
	if valueDiags.HasErrors() {
		diags = append(diags, valueDiags...)

		return diags
	}

	if !value.IsKnown() || value.IsNull() {
		platformRange := rs.Platform.Range()
		diags = append(diags, diagError("Invalid mobile platform", "platform must be a known string literal.", &platformRange))

		return diags
	}

	if value.Type().FriendlyName() != "string" {
		platformRange := rs.Platform.Range()
		diags = append(diags, diagError("Invalid mobile platform", "platform must be a string literal such as \"ios\".", &platformRange))

		return diags
	}

	platform := value.AsString()

	if platform != supportedMobilePlatform {
		platformRange := rs.Platform.Range()
		diags = append(diags, diagError("Unsupported mobile platform", fmt.Sprintf("mobile platform %q is not supported yet, use \"ios\".", platform), &platformRange))
	}

	return diags
}

func mobileVisibilityFromBlock(path string, v *visibleBlock) model.MobileVisibility {
	if v == nil {
		return model.MobileVisibility{}
	}

	return model.MobileVisibility{
		ID:       expr(path, v.ID),
		Timeout:  expr(path, v.Timeout),
		Interval: expr(path, v.Interval),
	}
}

func mobileValueExpectationFromBlock(path string, v *valueBlock) model.MobileValueExpectation {
	if v == nil {
		return model.MobileValueExpectation{}
	}

	return model.MobileValueExpectation{
		ID:       expr(path, v.ID),
		Expected: expr(path, v.Value),
		Timeout:  expr(path, v.Timeout),
		Interval: expr(path, v.Interval),
	}
}

func mobileStateExpectationFromBlock(path string, v *stateBlock) model.MobileStateExpectation {
	if v == nil {
		return model.MobileStateExpectation{}
	}

	return model.MobileStateExpectation{
		ID:       expr(path, v.ID),
		Timeout:  expr(path, v.Timeout),
		Interval: expr(path, v.Interval),
	}
}

// decodeMobileActions walks the actions body in source order using hclsyntax,
// preserving the textual order of tap/input_text/clear_text directives.
func decodeMobileActions(path string, body hcl.Body) ([]model.MobileAction, hcl.Diagnostics) {
	diags := make(hcl.Diagnostics, 0)

	syntaxBody, ok := body.(*hclsyntax.Body)
	if !ok {
		diags = append(diags, diagError("Unsupported actions block", "mobile actions block must use HCL native syntax.", nil))

		return nil, diags
	}

	for name, attr := range syntaxBody.Attributes {
		attrRange := attr.Range()
		diags = append(diags, diagError("Unknown actions attribute", fmt.Sprintf("attribute %q is not allowed inside actions; use tap, input_text, clear_text, wait_visible, or wait_not_visible blocks.", name), &attrRange))
	}

	actions := make([]model.MobileAction, 0, len(syntaxBody.Blocks))

	for _, block := range syntaxBody.Blocks {
		action, blockDiags := decodeMobileActionBlock(path, block)
		diags = append(diags, blockDiags...)

		if action != nil {
			actions = append(actions, *action)
		}
	}

	return actions, diags
}

func decodeMobileActionBlock(path string, block *hclsyntax.Block) (*model.MobileAction, hcl.Diagnostics) {
	diags := make(hcl.Diagnostics, 0)

	switch block.Type {
	case string(model.MobileActionTap):
		return decodeTapBlock(path, block)
	case string(model.MobileActionInputText):
		return decodeInputTextBlock(path, block)
	case string(model.MobileActionClearText):
		return decodeClearTextBlock(path, block)
	case string(model.MobileActionWaitVisible):
		return decodeWaitBlock(path, block, model.MobileActionWaitVisible)
	case string(model.MobileActionWaitNotVisible):
		return decodeWaitBlock(path, block, model.MobileActionWaitNotVisible)
	default:
		blockRange := block.DefRange()
		diags = append(diags, diagError("Unknown action", fmt.Sprintf("action %q is not supported; use tap, input_text, clear_text, wait_visible, or wait_not_visible.", block.Type), &blockRange))

		return nil, diags
	}
}

func decodeTapBlock(path string, block *hclsyntax.Block) (*model.MobileAction, hcl.Diagnostics) {
	diags := make(hcl.Diagnostics, 0)

	idExpr, idDiags := requireActionAttr(block, "tap", "id")
	diags = append(diags, idDiags...)

	timeoutExpr := hcl.Expression(nil)
	intervalExpr := hcl.Expression(nil)

	for name, attr := range block.Body.Attributes {
		if name == "id" {
			continue
		}

		if name == mobileTimeoutAttr {
			timeoutExpr = attr.Expr

			continue
		}

		if name == mobileIntervalAttr {
			intervalExpr = attr.Expr

			continue
		}

		attrRange := attr.Range()
		diags = append(diags, diagError("Unknown tap attribute", fmt.Sprintf("tap attribute %q is not supported; allowed: id, timeout, interval.", name), &attrRange))
	}

	action := &model.MobileAction{
		Kind:     model.MobileActionTap,
		File:     path,
		Line:     block.DefRange().Start.Line,
		ID:       expr(path, idExpr),
		Timeout:  expr(path, timeoutExpr),
		Interval: expr(path, intervalExpr),
	}

	return action, diags
}

func decodeInputTextBlock(path string, block *hclsyntax.Block) (*model.MobileAction, hcl.Diagnostics) {
	diags := make(hcl.Diagnostics, 0)

	idExpr, idDiags := requireActionAttr(block, "input_text", "id")
	diags = append(diags, idDiags...)

	valueExpr, valueDiags := requireActionAttr(block, "input_text", "value")
	diags = append(diags, valueDiags...)

	var (
		secureExpr   hcl.Expression
		timeoutExpr  hcl.Expression
		intervalExpr hcl.Expression
	)

	for name, attr := range block.Body.Attributes {
		switch name {
		case "id", "value":
			continue
		case "secure":
			secureExpr = attr.Expr
		case mobileTimeoutAttr:
			timeoutExpr = attr.Expr
		case mobileIntervalAttr:
			intervalExpr = attr.Expr
		default:
			attrRange := attr.Range()
			diags = append(diags, diagError("Unknown input_text attribute", fmt.Sprintf("input_text attribute %q is not supported; allowed: id, value, secure, timeout, interval.", name), &attrRange))
		}
	}

	action := &model.MobileAction{
		Kind:     model.MobileActionInputText,
		File:     path,
		Line:     block.DefRange().Start.Line,
		ID:       expr(path, idExpr),
		Value:    expr(path, valueExpr),
		Secure:   expr(path, secureExpr),
		Timeout:  expr(path, timeoutExpr),
		Interval: expr(path, intervalExpr),
	}

	return action, diags
}

func decodeClearTextBlock(path string, block *hclsyntax.Block) (*model.MobileAction, hcl.Diagnostics) {
	diags := make(hcl.Diagnostics, 0)

	idExpr, idDiags := requireActionAttr(block, "clear_text", "id")
	diags = append(diags, idDiags...)

	timeoutExpr := hcl.Expression(nil)
	intervalExpr := hcl.Expression(nil)

	for name, attr := range block.Body.Attributes {
		if name == "id" {
			continue
		}

		if name == mobileTimeoutAttr {
			timeoutExpr = attr.Expr

			continue
		}

		if name == mobileIntervalAttr {
			intervalExpr = attr.Expr

			continue
		}

		attrRange := attr.Range()
		diags = append(diags, diagError("Unknown clear_text attribute", fmt.Sprintf("clear_text attribute %q is not supported; allowed: id, timeout, interval.", name), &attrRange))
	}

	action := &model.MobileAction{
		Kind:     model.MobileActionClearText,
		File:     path,
		Line:     block.DefRange().Start.Line,
		ID:       expr(path, idExpr),
		Timeout:  expr(path, timeoutExpr),
		Interval: expr(path, intervalExpr),
	}

	return action, diags
}

func decodeWaitBlock(path string, block *hclsyntax.Block, kind model.MobileActionKind) (*model.MobileAction, hcl.Diagnostics) {
	diags := make(hcl.Diagnostics, 0)
	actionName := string(kind)

	idExpr, idDiags := requireActionAttr(block, actionName, "id")
	diags = append(diags, idDiags...)

	timeoutExpr := hcl.Expression(nil)
	intervalExpr := hcl.Expression(nil)

	for name, attr := range block.Body.Attributes {
		switch name {
		case "id":
			continue
		case mobileTimeoutAttr:
			timeoutExpr = attr.Expr
		case mobileIntervalAttr:
			intervalExpr = attr.Expr
		default:
			attrRange := attr.Range()
			diags = append(diags, diagError("Unknown "+actionName+" attribute", fmt.Sprintf("%s attribute %q is not supported; allowed: id, timeout, interval.", actionName, name), &attrRange))
		}
	}

	action := &model.MobileAction{
		Kind:     kind,
		File:     path,
		Line:     block.DefRange().Start.Line,
		ID:       expr(path, idExpr),
		Timeout:  expr(path, timeoutExpr),
		Interval: expr(path, intervalExpr),
	}

	return action, diags
}

func requireActionAttr(block *hclsyntax.Block, action, name string) (hcl.Expression, hcl.Diagnostics) {
	attr, ok := block.Body.Attributes[name]
	if !ok {
		blockRange := block.DefRange()

		return nil, hcl.Diagnostics{diagError(fmt.Sprintf("Missing %s attribute", action), fmt.Sprintf("%s block must declare %q.", action, name), &blockRange)}
	}

	return attr.Expr, nil
}
