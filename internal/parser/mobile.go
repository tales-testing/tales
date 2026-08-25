package parser

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/tales-testing/tales/internal/model"
)

// mobileProviderType is the provider label that triggers mobile step decoding.
const mobileProviderType = "mobile"

// supportedMobilePlatforms are the platform names a mobile step may
// declare, in the order error messages list them.
//
// The parser only checks the name is one Tales knows; whether the
// running binary carries a backend for it is a runtime question, and
// answering it here would make `tales validate` depend on how the
// binary was built.
var supportedMobilePlatforms = []string{"android", "ios"}

const mobileTimeoutAttr = "timeout"
const mobileIntervalAttr = "interval"
const mobileValueAttr = "value"
const mobileDurationAttr = "duration"
const mobileFirstAttr = "first"
const mobileLabelAttr = "label"
const mobileTextLocAttr = "text"

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

	if rs.Permissions != nil {
		perms, pDiags := decodeMobilePermissions(path, rs.Permissions.Body)
		diags = append(diags, pDiags...)
		ms.Permissions = perms
	}

	expectBody := rs.Expect
	if expectBody == nil {
		expectBody = rs.Response
	}

	if expectBody != nil {
		diags = append(diags, rejectBrowserExpectBlocks(expectBody)...)

		for _, v := range expectBody.Visible {
			diags = append(diags, rejectBrowserSelector(v.Selector, "visible")...)

			visible, vDiags := mobileVisibilityFromBlock(path, "visible", v)
			diags = append(diags, vDiags...)
			ms.Expect.Visible = append(ms.Expect.Visible, visible)
		}

		for _, v := range expectBody.NotVisible {
			diags = append(diags, rejectBrowserSelector(v.Selector, "not_visible")...)

			notVisible, vDiags := mobileVisibilityFromBlock(path, "not_visible", v)
			diags = append(diags, vDiags...)
			ms.Expect.NotVisible = append(ms.Expect.NotVisible, notVisible)
		}

		for _, v := range expectBody.Text {
			diags = append(diags, rejectBrowserSelector(v.Selector, "text")...)

			text, vDiags := mobileValueExpectationFromBlock(path, "text", v)
			diags = append(diags, vDiags...)
			ms.Expect.Text = append(ms.Expect.Text, text)
		}

		for _, v := range expectBody.Value {
			diags = append(diags, rejectBrowserSelector(v.Selector, "value")...)

			value, vDiags := mobileValueExpectationFromBlock(path, "value", v)
			diags = append(diags, vDiags...)
			ms.Expect.Value = append(ms.Expect.Value, value)
		}

		for _, v := range expectBody.Enabled {
			diags = append(diags, rejectBrowserSelector(v.Selector, "enabled")...)

			enabled, vDiags := mobileStateExpectationFromBlock(path, "enabled", v)
			diags = append(diags, vDiags...)
			ms.Expect.Enabled = append(ms.Expect.Enabled, enabled)
		}

		for _, v := range expectBody.Disabled {
			diags = append(diags, rejectBrowserSelector(v.Selector, "disabled")...)

			disabled, vDiags := mobileStateExpectationFromBlock(path, "disabled", v)
			diags = append(diags, vDiags...)
			ms.Expect.Disabled = append(ms.Expect.Disabled, disabled)
		}
	}

	return ms, diags
}

// rejectBrowserSelector emits a diag when a mobile expect block uses the
// browser-style "selector" attribute. Mobile locators are accessibility ids.
func rejectBrowserSelector(selector hcl.Expression, blockName string) hcl.Diagnostics {
	if !exprIsSet(selector) {
		return nil
	}

	rng := selector.Range()

	return hcl.Diagnostics{diagError(
		"Unexpected selector attribute",
		fmt.Sprintf("mobile %s block uses id (accessibility id), not selector. Did you mean to use provider \"browser\"?", blockName),
		&rng,
	)}
}

// rejectBrowserExpectBlocks emits diags when a mobile expect carries
// browser-only blocks (attribute / url / title).
func rejectBrowserExpectBlocks(expect *expectBlock) hcl.Diagnostics {
	diags := make(hcl.Diagnostics, 0)

	for _, b := range expect.Attribute {
		if b == nil {
			continue
		}

		rng := b.Selector.Range()
		diags = append(diags, diagError(
			"Unexpected attribute block",
			"attribute expectation is browser-only; mobile steps do not support attribute matching.",
			&rng,
		))
	}

	for _, b := range expect.URL {
		if b == nil {
			continue
		}

		rng := b.Value.Range()
		diags = append(diags, diagError(
			"Unexpected url block",
			"url expectation is browser-only; mobile steps do not support URL matching.",
			&rng,
		))
	}

	for _, b := range expect.Title {
		if b == nil {
			continue
		}

		rng := b.Value.Range()
		diags = append(diags, diagError(
			"Unexpected title block",
			"title expectation is browser-only; mobile steps do not support title matching.",
			&rng,
		))
	}

	for _, b := range expect.WebPerf {
		if b == nil {
			continue
		}

		rng := b.Body.MissingItemRange()
		diags = append(diags, diagError(
			"Unexpected web_perf block",
			"web_perf expectation is browser-only; mobile steps do not support web performance assertions.",
			&rng,
		))
	}

	return diags
}

// looksLikeMobileStep checks for fields that only the mobile provider uses,
// so the dispatcher can flag a misrouted step. target/actions/expect are
// shared with the browser provider and intentionally NOT included here.
func looksLikeMobileStep(rs stepBlock) bool {
	if rs.Provider == mobileProviderType {
		return true
	}

	if exprIsSet(rs.Platform) {
		return true
	}

	if rs.Launch != nil || rs.Terminate != nil || rs.Permissions != nil {
		return true
	}

	return false
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
		diags = append(diags, diagError(
			"Missing mobile platform",
			fmt.Sprintf("mobile step must declare platform = one of %s.", strings.Join(supportedMobilePlatforms, ", ")),
			nil,
		))

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

	if !slices.Contains(supportedMobilePlatforms, platform) {
		platformRange := rs.Platform.Range()
		diags = append(diags, diagError(
			"Unsupported mobile platform",
			fmt.Sprintf("mobile platform %q is not supported; use one of %s.",
				platform, strings.Join(supportedMobilePlatforms, ", ")),
			&platformRange,
		))
	}

	return diags
}

// mobileLocatorNames lists the element locators, in the order messages
// should present them: most stable first.
var mobileLocatorNames = []string{"id", "label", "text"}

// requireExpectLocator enforces that exactly one locator is set on an
// expect block, mirroring requireIDOrLabel for actions. The expect
// blocks are decoded via gohcl so the expressions are pre-extracted; the
// helper only validates them and emits diags through the block name.
func requireExpectLocator(blockName string, idExpr, labelExpr, textExpr hcl.Expression) hcl.Diagnostics {
	set := make([]hcl.Expression, 0, len(mobileLocatorNames))

	for _, e := range []hcl.Expression{idExpr, labelExpr, textExpr} {
		if exprIsSet(e) {
			set = append(set, e)
		}
	}

	switch {
	case len(set) > 1:
		// Point at the second one: it is the attribute the author added
		// to an already-complete locator.
		conflictRange := set[1].Range()

		return hcl.Diagnostics{diagError(
			"Conflicting element locator",
			fmt.Sprintf("%s block must declare exactly one of %s.", blockName, strings.Join(mobileLocatorNames, ", ")),
			&conflictRange,
		)}
	case len(set) == 0:
		return hcl.Diagnostics{diagError(
			"Missing element locator",
			fmt.Sprintf("%s block must declare one of %s.", blockName, strings.Join(mobileLocatorNames, ", ")),
			nil,
		)}
	}

	return nil
}

func mobileVisibilityFromBlock(path, blockName string, v *visibleBlock) (model.MobileVisibility, hcl.Diagnostics) {
	if v == nil {
		return model.MobileVisibility{}, nil
	}

	diags := requireExpectLocator(blockName, v.ID, v.Label, v.Text)

	return model.MobileVisibility{
		ID:       optionalExpr(path, v.ID),
		Label:    optionalExpr(path, v.Label),
		Text:     optionalExpr(path, v.Text),
		Timeout:  optionalExpr(path, v.Timeout),
		Interval: optionalExpr(path, v.Interval),
	}, diags
}

func mobileValueExpectationFromBlock(path, blockName string, v *valueBlock) (model.MobileValueExpectation, hcl.Diagnostics) {
	if v == nil {
		return model.MobileValueExpectation{}, nil
	}

	diags := requireExpectLocator(blockName, v.ID, v.Label, v.Text)

	return model.MobileValueExpectation{
		ID:       optionalExpr(path, v.ID),
		Label:    optionalExpr(path, v.Label),
		Text:     optionalExpr(path, v.Text),
		Expected: optionalExpr(path, v.Value),
		Timeout:  optionalExpr(path, v.Timeout),
		Interval: optionalExpr(path, v.Interval),
	}, diags
}

func mobileStateExpectationFromBlock(path, blockName string, v *stateBlock) (model.MobileStateExpectation, hcl.Diagnostics) {
	if v == nil {
		return model.MobileStateExpectation{}, nil
	}

	diags := requireExpectLocator(blockName, v.ID, v.Label, v.Text)

	return model.MobileStateExpectation{
		ID:       optionalExpr(path, v.ID),
		Label:    optionalExpr(path, v.Label),
		Text:     optionalExpr(path, v.Text),
		Timeout:  optionalExpr(path, v.Timeout),
		Interval: optionalExpr(path, v.Interval),
	}, diags
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
		diags = append(diags, diagError("Unknown actions attribute", fmt.Sprintf("attribute %q is not allowed inside actions; use tap, double_tap, long_press, input_text, clear_text, swipe, scroll, press_key, press_button, set_orientation, wait_visible, wait_not_visible, wait_enabled, or wait_disabled blocks.", name), &attrRange))
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

// decodeMobilePermissions walks the permissions body: each attribute is a
// privacy service name mapped to an "allow" / "deny" expression. Entries
// are sorted by service name so the decoded order is deterministic.
func decodeMobilePermissions(path string, body hcl.Body) ([]model.MobilePermission, hcl.Diagnostics) {
	diags := make(hcl.Diagnostics, 0)

	syntaxBody, ok := body.(*hclsyntax.Body)
	if !ok {
		diags = append(diags, diagError("Unsupported permissions block", "mobile permissions block must use HCL native syntax.", nil))

		return nil, diags
	}

	for _, block := range syntaxBody.Blocks {
		blockRange := block.DefRange()
		diags = append(diags, diagError("Unknown permissions block", fmt.Sprintf("nested block %q is not allowed inside permissions; use `service = \"allow\"` or `service = \"deny\"` attributes.", block.Type), &blockRange))
	}

	perms := make([]model.MobilePermission, 0, len(syntaxBody.Attributes))
	for name, attr := range syntaxBody.Attributes {
		perms = append(perms, model.MobilePermission{
			Service:  name,
			Decision: expr(path, attr.Expr),
			File:     path,
			Line:     attr.Range().Start.Line,
		})
	}

	sort.Slice(perms, func(i, j int) bool { return perms[i].Service < perms[j].Service })

	return perms, diags
}

func decodeMobileActionBlock(path string, block *hclsyntax.Block) (*model.MobileAction, hcl.Diagnostics) {
	diags := make(hcl.Diagnostics, 0)

	switch block.Type {
	case string(model.MobileActionTap):
		return decodeTapBlock(path, block)
	case string(model.MobileActionDoubleTap):
		return decodeDoubleTapBlock(path, block)
	case string(model.MobileActionLongPress):
		return decodeLongPressBlock(path, block)
	case string(model.MobileActionInputText):
		return decodeInputTextBlock(path, block)
	case string(model.MobileActionClearText):
		return decodeClearTextBlock(path, block)
	case string(model.MobileActionSwipe):
		return decodeSwipeBlock(path, block, model.MobileActionSwipe)
	case string(model.MobileActionScroll):
		return decodeSwipeBlock(path, block, model.MobileActionScroll)
	case string(model.MobileActionPressKey):
		return decodeDeviceActionBlock(path, block, model.MobileActionPressKey, "key")
	case string(model.MobileActionPressButton):
		return decodeDeviceActionBlock(path, block, model.MobileActionPressButton, "button")
	case string(model.MobileActionSetOrientation):
		return decodeDeviceActionBlock(path, block, model.MobileActionSetOrientation, "orientation")
	case string(model.MobileActionWaitVisible):
		return decodeWaitBlock(path, block, model.MobileActionWaitVisible)
	case string(model.MobileActionWaitNotVisible):
		return decodeWaitBlock(path, block, model.MobileActionWaitNotVisible)
	case string(model.MobileActionWaitEnabled):
		return decodeWaitBlock(path, block, model.MobileActionWaitEnabled)
	case string(model.MobileActionWaitDisabled):
		return decodeWaitBlock(path, block, model.MobileActionWaitDisabled)
	case string(model.MobileActionDismissKeyboard):
		return decodeDismissKeyboardBlock(path, block)
	case string(model.MobileActionScrollTo):
		return decodeScrollToBlock(path, block)
	default:
		blockRange := block.DefRange()
		diags = append(diags, diagError("Unknown action", fmt.Sprintf("action %q is not supported; use tap, double_tap, long_press, input_text, clear_text, swipe, scroll, press_key, press_button, set_orientation, dismiss_keyboard, scroll_to, wait_visible, wait_not_visible, wait_enabled, or wait_disabled.", block.Type), &blockRange))

		return nil, diags
	}
}

// decodeScrollToBlock parses a `scroll_to { id | label | text }` action.
// The locator is XOR-validated like every other element-targeted action;
// no other attributes are accepted (no timeout / interval — the driver
// bounds its own scroll attempts, so there is no polling loop to size
// from the scenario).
func decodeScrollToBlock(path string, block *hclsyntax.Block) (*model.MobileAction, hcl.Diagnostics) {
	diags := make(hcl.Diagnostics, 0, len(block.Body.Attributes)+len(block.Body.Blocks))

	locator, locatorDiags := requireActionLocator(block, "scroll_to")
	diags = append(diags, locatorDiags...)

	for name, attr := range block.Body.Attributes {
		if name == "id" || name == mobileLabelAttr || name == mobileTextLocAttr {
			continue
		}

		attrRange := attr.Range()
		diags = append(diags, diagError(
			"Unknown scroll_to attribute",
			fmt.Sprintf("scroll_to attribute %q is not supported; allowed: id, label, text.", name),
			&attrRange,
		))
	}

	for _, sub := range block.Body.Blocks {
		subRange := sub.DefRange()
		diags = append(diags, diagError(
			"Unknown scroll_to block",
			fmt.Sprintf("scroll_to takes no nested blocks; remove %q.", sub.Type),
			&subRange,
		))
	}

	action := &model.MobileAction{
		Kind:  model.MobileActionScrollTo,
		File:  path,
		Line:  block.DefRange().Start.Line,
		ID:    expr(path, locator.ID),
		Label: expr(path, locator.Label),
		Text:  expr(path, locator.Text),
	}

	return action, diags
}

// decodeDismissKeyboardBlock parses an empty `dismiss_keyboard {}` action.
// It takes no element locator (no id / label / value / direction) — the
// driver targets the focused first responder. Accepts no attributes; any
// content surfaces an actionable diagnostic so authors don't silently
// pass timeouts on a fire-and-forget op.
func decodeDismissKeyboardBlock(path string, block *hclsyntax.Block) (*model.MobileAction, hcl.Diagnostics) {
	diags := make(hcl.Diagnostics, 0, len(block.Body.Attributes)+len(block.Body.Blocks))

	for name, attr := range block.Body.Attributes {
		attrRange := attr.Range()
		diags = append(diags, diagError(
			"Unknown dismiss_keyboard attribute",
			fmt.Sprintf("dismiss_keyboard takes no attributes; remove %q.", name),
			&attrRange,
		))
	}

	for _, sub := range block.Body.Blocks {
		subRange := sub.DefRange()
		diags = append(diags, diagError(
			"Unknown dismiss_keyboard block",
			fmt.Sprintf("dismiss_keyboard takes no nested blocks; remove %q.", sub.Type),
			&subRange,
		))
	}

	action := &model.MobileAction{
		Kind: model.MobileActionDismissKeyboard,
		File: path,
		Line: block.DefRange().Start.Line,
	}

	return action, diags
}

func decodeTapBlock(path string, block *hclsyntax.Block) (*model.MobileAction, hcl.Diagnostics) {
	diags := make(hcl.Diagnostics, 0)

	locator, locatorDiags := requireActionLocator(block, "tap")
	diags = append(diags, locatorDiags...)

	timeoutExpr := hcl.Expression(nil)
	intervalExpr := hcl.Expression(nil)
	firstExpr := hcl.Expression(nil)

	for name, attr := range block.Body.Attributes {
		switch name {
		case "id", mobileLabelAttr, mobileTextLocAttr:
			continue
		case mobileTimeoutAttr:
			timeoutExpr = attr.Expr
		case mobileIntervalAttr:
			intervalExpr = attr.Expr
		case mobileFirstAttr:
			firstExpr = attr.Expr
		default:
			attrRange := attr.Range()
			diags = append(diags, diagError("Unknown tap attribute", fmt.Sprintf("tap attribute %q is not supported; allowed: id, label, text, timeout, interval, first.", name), &attrRange))
		}
	}

	action := &model.MobileAction{
		Kind:     model.MobileActionTap,
		File:     path,
		Line:     block.DefRange().Start.Line,
		ID:       expr(path, locator.ID),
		Label:    expr(path, locator.Label),
		Text:     expr(path, locator.Text),
		Timeout:  expr(path, timeoutExpr),
		Interval: expr(path, intervalExpr),
		First:    expr(path, firstExpr),
	}

	return action, diags
}

func decodeInputTextBlock(path string, block *hclsyntax.Block) (*model.MobileAction, hcl.Diagnostics) {
	diags := make(hcl.Diagnostics, 0)

	locator, locatorDiags := requireActionLocator(block, "input_text")
	diags = append(diags, locatorDiags...)

	valueExpr, valueDiags := requireActionAttr(block, "input_text", mobileValueAttr)
	diags = append(diags, valueDiags...)

	var (
		secureExpr   hcl.Expression
		timeoutExpr  hcl.Expression
		intervalExpr hcl.Expression
		firstExpr    hcl.Expression
	)

	for name, attr := range block.Body.Attributes {
		switch name {
		case "id", mobileLabelAttr, mobileTextLocAttr, mobileValueAttr:
			continue
		case "secure":
			secureExpr = attr.Expr
		case mobileTimeoutAttr:
			timeoutExpr = attr.Expr
		case mobileIntervalAttr:
			intervalExpr = attr.Expr
		case mobileFirstAttr:
			firstExpr = attr.Expr
		default:
			attrRange := attr.Range()
			diags = append(diags, diagError("Unknown input_text attribute", fmt.Sprintf("input_text attribute %q is not supported; allowed: id, label, text, value, secure, timeout, interval, first.", name), &attrRange))
		}
	}

	action := &model.MobileAction{
		Kind:     model.MobileActionInputText,
		File:     path,
		Line:     block.DefRange().Start.Line,
		ID:       expr(path, locator.ID),
		Label:    expr(path, locator.Label),
		Text:     expr(path, locator.Text),
		Value:    expr(path, valueExpr),
		Secure:   expr(path, secureExpr),
		Timeout:  expr(path, timeoutExpr),
		Interval: expr(path, intervalExpr),
		First:    expr(path, firstExpr),
	}

	return action, diags
}

func decodeClearTextBlock(path string, block *hclsyntax.Block) (*model.MobileAction, hcl.Diagnostics) {
	diags := make(hcl.Diagnostics, 0)

	locator, locatorDiags := requireActionLocator(block, "clear_text")
	diags = append(diags, locatorDiags...)

	timeoutExpr := hcl.Expression(nil)
	intervalExpr := hcl.Expression(nil)
	firstExpr := hcl.Expression(nil)

	for name, attr := range block.Body.Attributes {
		switch name {
		case "id", mobileLabelAttr, mobileTextLocAttr:
			continue
		case mobileTimeoutAttr:
			timeoutExpr = attr.Expr
		case mobileIntervalAttr:
			intervalExpr = attr.Expr
		case mobileFirstAttr:
			firstExpr = attr.Expr
		default:
			attrRange := attr.Range()
			diags = append(diags, diagError("Unknown clear_text attribute", fmt.Sprintf("clear_text attribute %q is not supported; allowed: id, label, text, timeout, interval, first.", name), &attrRange))
		}
	}

	action := &model.MobileAction{
		Kind:     model.MobileActionClearText,
		File:     path,
		Line:     block.DefRange().Start.Line,
		ID:       expr(path, locator.ID),
		Label:    expr(path, locator.Label),
		Text:     expr(path, locator.Text),
		Timeout:  expr(path, timeoutExpr),
		Interval: expr(path, intervalExpr),
		First:    expr(path, firstExpr),
	}

	return action, diags
}

func decodeWaitBlock(path string, block *hclsyntax.Block, kind model.MobileActionKind) (*model.MobileAction, hcl.Diagnostics) {
	diags := make(hcl.Diagnostics, 0)
	actionName := string(kind)

	locator, locatorDiags := requireActionLocator(block, actionName)
	diags = append(diags, locatorDiags...)

	timeoutExpr := hcl.Expression(nil)
	intervalExpr := hcl.Expression(nil)
	firstExpr := hcl.Expression(nil)

	for name, attr := range block.Body.Attributes {
		switch name {
		case "id", mobileLabelAttr, mobileTextLocAttr:
			continue
		case mobileTimeoutAttr:
			timeoutExpr = attr.Expr
		case mobileIntervalAttr:
			intervalExpr = attr.Expr
		case mobileFirstAttr:
			firstExpr = attr.Expr
		default:
			attrRange := attr.Range()
			diags = append(diags, diagError("Unknown "+actionName+" attribute", fmt.Sprintf("%s attribute %q is not supported; allowed: id, label, text, timeout, interval, first.", actionName, name), &attrRange))
		}
	}

	action := &model.MobileAction{
		Kind:     kind,
		File:     path,
		Line:     block.DefRange().Start.Line,
		ID:       expr(path, locator.ID),
		Label:    expr(path, locator.Label),
		Text:     expr(path, locator.Text),
		Timeout:  expr(path, timeoutExpr),
		Interval: expr(path, intervalExpr),
		First:    expr(path, firstExpr),
	}

	return action, diags
}

func decodeDoubleTapBlock(path string, block *hclsyntax.Block) (*model.MobileAction, hcl.Diagnostics) {
	diags := make(hcl.Diagnostics, 0)

	locator, locatorDiags := requireActionLocator(block, "double_tap")
	diags = append(diags, locatorDiags...)

	timeoutExpr := hcl.Expression(nil)
	intervalExpr := hcl.Expression(nil)
	firstExpr := hcl.Expression(nil)

	for name, attr := range block.Body.Attributes {
		switch name {
		case "id", mobileLabelAttr, mobileTextLocAttr:
			continue
		case mobileTimeoutAttr:
			timeoutExpr = attr.Expr
		case mobileIntervalAttr:
			intervalExpr = attr.Expr
		case mobileFirstAttr:
			firstExpr = attr.Expr
		default:
			attrRange := attr.Range()
			diags = append(diags, diagError("Unknown double_tap attribute", fmt.Sprintf("double_tap attribute %q is not supported; allowed: id, label, text, timeout, interval, first.", name), &attrRange))
		}
	}

	action := &model.MobileAction{
		Kind:     model.MobileActionDoubleTap,
		File:     path,
		Line:     block.DefRange().Start.Line,
		ID:       expr(path, locator.ID),
		Label:    expr(path, locator.Label),
		Text:     expr(path, locator.Text),
		Timeout:  expr(path, timeoutExpr),
		Interval: expr(path, intervalExpr),
		First:    expr(path, firstExpr),
	}

	return action, diags
}

func decodeLongPressBlock(path string, block *hclsyntax.Block) (*model.MobileAction, hcl.Diagnostics) {
	diags := make(hcl.Diagnostics, 0)

	locator, locatorDiags := requireActionLocator(block, "long_press")
	diags = append(diags, locatorDiags...)

	var durationExpr, timeoutExpr, intervalExpr, firstExpr hcl.Expression

	for name, attr := range block.Body.Attributes {
		switch name {
		case "id", mobileLabelAttr, mobileTextLocAttr:
			continue
		case mobileDurationAttr:
			durationExpr = attr.Expr
		case mobileTimeoutAttr:
			timeoutExpr = attr.Expr
		case mobileIntervalAttr:
			intervalExpr = attr.Expr
		case mobileFirstAttr:
			firstExpr = attr.Expr
		default:
			attrRange := attr.Range()
			diags = append(diags, diagError("Unknown long_press attribute", fmt.Sprintf("long_press attribute %q is not supported; allowed: id, label, text, duration, timeout, interval, first.", name), &attrRange))
		}
	}

	action := &model.MobileAction{
		Kind:     model.MobileActionLongPress,
		File:     path,
		Line:     block.DefRange().Start.Line,
		ID:       expr(path, locator.ID),
		Label:    expr(path, locator.Label),
		Text:     expr(path, locator.Text),
		Duration: expr(path, durationExpr),
		Timeout:  expr(path, timeoutExpr),
		Interval: expr(path, intervalExpr),
		First:    expr(path, firstExpr),
	}

	return action, diags
}

func decodeSwipeBlock(path string, block *hclsyntax.Block, kind model.MobileActionKind) (*model.MobileAction, hcl.Diagnostics) {
	diags := make(hcl.Diagnostics, 0)
	actionName := string(kind)

	locator, locatorDiags := requireActionLocator(block, actionName)
	diags = append(diags, locatorDiags...)

	directionExpr, dirDiags := requireActionAttr(block, actionName, "direction")
	diags = append(diags, dirDiags...)

	var distanceExpr, durationExpr, timeoutExpr, intervalExpr, firstExpr hcl.Expression

	for name, attr := range block.Body.Attributes {
		switch name {
		case "id", mobileLabelAttr, mobileTextLocAttr, "direction":
			continue
		case "distance":
			distanceExpr = attr.Expr
		case mobileDurationAttr:
			durationExpr = attr.Expr
		case mobileTimeoutAttr:
			timeoutExpr = attr.Expr
		case mobileIntervalAttr:
			intervalExpr = attr.Expr
		case mobileFirstAttr:
			firstExpr = attr.Expr
		default:
			attrRange := attr.Range()
			diags = append(diags, diagError("Unknown "+actionName+" attribute", fmt.Sprintf("%s attribute %q is not supported; allowed: id, label, text, direction, distance, duration, timeout, interval, first.", actionName, name), &attrRange))
		}
	}

	action := &model.MobileAction{
		Kind:      kind,
		File:      path,
		Line:      block.DefRange().Start.Line,
		ID:        expr(path, locator.ID),
		Label:     expr(path, locator.Label),
		Text:      expr(path, locator.Text),
		Direction: expr(path, directionExpr),
		Distance:  expr(path, distanceExpr),
		Duration:  expr(path, durationExpr),
		Timeout:   expr(path, timeoutExpr),
		Interval:  expr(path, intervalExpr),
		First:     expr(path, firstExpr),
	}

	return action, diags
}

// decodeDeviceActionBlock decodes a device-level action (press_key,
// press_button, set_orientation) that carries a single required string
// attribute (key / button / orientation) and no element id. The argument
// is stored in MobileAction.Value.
func decodeDeviceActionBlock(path string, block *hclsyntax.Block, kind model.MobileActionKind, argName string) (*model.MobileAction, hcl.Diagnostics) {
	diags := make(hcl.Diagnostics, 0)
	actionName := string(kind)

	argExpr, argDiags := requireActionAttr(block, actionName, argName)
	diags = append(diags, argDiags...)

	timeoutExpr := hcl.Expression(nil)
	intervalExpr := hcl.Expression(nil)

	for name, attr := range block.Body.Attributes {
		switch name {
		case argName:
			continue
		case mobileTimeoutAttr:
			timeoutExpr = attr.Expr
		case mobileIntervalAttr:
			intervalExpr = attr.Expr
		default:
			attrRange := attr.Range()
			diags = append(diags, diagError("Unknown "+actionName+" attribute", fmt.Sprintf("%s attribute %q is not supported; allowed: %s, timeout, interval.", actionName, name, argName), &attrRange))
		}
	}

	action := &model.MobileAction{
		Kind:     kind,
		File:     path,
		Line:     block.DefRange().Start.Line,
		Value:    expr(path, argExpr),
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

// actionLocator holds the element locator read off an action body.
// Exactly one field is set on a well-formed action.
type actionLocator struct {
	ID    hcl.Expression
	Label hcl.Expression
	Text  hcl.Expression
}

// requireActionLocator reads the locator attributes on an action body
// and enforces that exactly one is set. Returns the bound expressions
// (nil-safe) plus any diagnostics. Used by every action that resolves an
// element so the surface stays consistent.
func requireActionLocator(block *hclsyntax.Block, action string) (actionLocator, hcl.Diagnostics) {
	var (
		locator actionLocator
		present []*hclsyntax.Attribute
	)

	bind := map[string]*hcl.Expression{
		"id":              &locator.ID,
		mobileLabelAttr:   &locator.Label,
		mobileTextLocAttr: &locator.Text,
	}

	// Iterate the declared names rather than the map so the order the
	// conflict is reported in does not depend on map iteration.
	for _, name := range mobileLocatorNames {
		attr, ok := block.Body.Attributes[name]
		if !ok {
			continue
		}

		present = append(present, attr)
		*bind[name] = attr.Expr
	}

	switch {
	case len(present) > 1:
		// Point at the second one: it is the attribute the author added
		// to an already-complete locator.
		conflictRange := present[1].Range()

		return locator, hcl.Diagnostics{diagError(
			"Conflicting element locator",
			fmt.Sprintf("%s block must declare exactly one of %s.", action, strings.Join(mobileLocatorNames, ", ")),
			&conflictRange,
		)}
	case len(present) == 0:
		blockRange := block.DefRange()

		return actionLocator{}, hcl.Diagnostics{diagError(
			"Missing element locator",
			fmt.Sprintf("%s block must declare one of %s.", action, strings.Join(mobileLocatorNames, ", ")),
			&blockRange,
		)}
	}

	return locator, nil
}
