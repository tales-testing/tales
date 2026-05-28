package parser

import (
	"fmt"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/tales-testing/tales/internal/model"
)

// browserProviderType is the provider label that triggers browser step
// decoding.
const browserProviderType = "browser"

const (
	browserTimeoutAttr  = "timeout"
	browserIntervalAttr = "interval"
	browserSelectorAttr = "selector"
	browserValueAttr    = "value"
	browserURLAttr      = "url"
	browserKeyAttr      = "key"
	browserSecureAttr   = "secure"
)

// decodeBrowserStepIfNeeded is the dispatcher called by decodeSteps. It
// decodes a browser-shaped step when provider == "browser", and refuses
// browser-or-mobile-only fields on unsupported providers. When a step
// matches the mobile heuristic, decoding is deferred to the mobile
// dispatcher so the user gets a mobile-specific diagnostic.
func decodeBrowserStepIfNeeded(path string, rs stepBlock, stepName string) (*model.BrowserStep, hcl.Diagnostics) {
	if rs.Provider == browserProviderType {
		return decodeBrowserStep(path, rs)
	}

	if looksLikeMobileStep(rs) {
		return nil, nil
	}

	if !looksLikeBrowserStep(rs) {
		return nil, nil
	}

	return nil, hcl.Diagnostics{diagError(
		"Browser fields on non-browser step",
		fmt.Sprintf("Step %q uses browser-only fields (actions, target, or attribute/url/title expectations) but its provider is %q; use provider \"browser\".", stepName, rs.Provider),
		nil,
	)}
}

// looksLikeBrowserStep returns true when the step carries fields that only
// the browser provider uses. `target` and `actions` are shared with mobile;
// the caller is expected to defer to the mobile dispatcher first when those
// alone are present, so browser claims them only after mobile passes.
func looksLikeBrowserStep(rs stepBlock) bool {
	if rs.Provider == browserProviderType {
		return true
	}

	if rs.Actions != nil || exprIsSet(rs.Target) {
		return true
	}

	expectBody := rs.Expect
	if expectBody == nil {
		expectBody = rs.Response
	}

	if expectBody != nil {
		if len(expectBody.Attribute) > 0 || len(expectBody.URL) > 0 || len(expectBody.Title) > 0 {
			return true
		}
	}

	return false
}

// decodeBrowserStep builds a model.BrowserStep from the parsed step block.
func decodeBrowserStep(path string, rs stepBlock) (*model.BrowserStep, hcl.Diagnostics) {
	diags := make(hcl.Diagnostics, 0)
	bs := &model.BrowserStep{}

	// target is optional at parse time: when omitted, ResolveTarget()
	// uses the single-target shortcut (the sole entry in
	// config.browser.targets). Missing config / ambiguous multi-target
	// are reported at execution time with a precise message.
	if exprIsSet(rs.Target) {
		bs.Target = expr(path, rs.Target)
	}

	if rs.Actions != nil {
		actions, aDiags := decodeBrowserActions(path, rs.Actions.Body)
		diags = append(diags, aDiags...)
		bs.Actions = actions
	}

	expectBody := rs.Expect
	if expectBody == nil {
		expectBody = rs.Response
	}

	if expectBody != nil {
		exDiags := decodeBrowserExpect(path, expectBody, &bs.Expect)
		diags = append(diags, exDiags...)
	}

	return bs, diags
}

// decodeBrowserActions walks the actions body in source order using
// hclsyntax, preserving the textual order of action directives.
func decodeBrowserActions(path string, body hcl.Body) ([]model.BrowserAction, hcl.Diagnostics) {
	diags := make(hcl.Diagnostics, 0)

	syntaxBody, ok := body.(*hclsyntax.Body)
	if !ok {
		diags = append(diags, diagError("Unsupported actions block", "browser actions block must use HCL native syntax.", nil))

		return nil, diags
	}

	for name, attr := range syntaxBody.Attributes {
		attrRange := attr.Range()
		diags = append(diags, diagError(
			"Unknown actions attribute",
			fmt.Sprintf("attribute %q is not allowed inside actions; use goto, click, fill, clear, press, submit, scroll, wait_visible, wait_not_visible, hover, select, check, uncheck, reload, back, or forward blocks.", name),
			&attrRange,
		))
	}

	actions := make([]model.BrowserAction, 0, len(syntaxBody.Blocks))

	for _, block := range syntaxBody.Blocks {
		action, blockDiags := decodeBrowserActionBlock(path, block)
		diags = append(diags, blockDiags...)

		if action != nil {
			actions = append(actions, *action)
		}
	}

	return actions, diags
}

func decodeBrowserActionBlock(path string, block *hclsyntax.Block) (*model.BrowserAction, hcl.Diagnostics) {
	switch block.Type {
	case string(model.BrowserActionGoto):
		return decodeBrowserGoto(path, block)
	case string(model.BrowserActionClick):
		return decodeBrowserSelectorOnly(path, block, model.BrowserActionClick)
	case string(model.BrowserActionFill):
		return decodeBrowserFill(path, block)
	case string(model.BrowserActionClear):
		return decodeBrowserSelectorOnly(path, block, model.BrowserActionClear)
	case string(model.BrowserActionPress):
		return decodeBrowserPress(path, block)
	case string(model.BrowserActionSubmit):
		return decodeBrowserSelectorOnly(path, block, model.BrowserActionSubmit)
	case string(model.BrowserActionScroll):
		return decodeBrowserScroll(path, block)
	case string(model.BrowserActionWaitVisible):
		return decodeBrowserSelectorOnly(path, block, model.BrowserActionWaitVisible)
	case string(model.BrowserActionWaitNotVisible):
		return decodeBrowserSelectorOnly(path, block, model.BrowserActionWaitNotVisible)
	case string(model.BrowserActionHover):
		return decodeBrowserSelectorOnly(path, block, model.BrowserActionHover)
	case string(model.BrowserActionSelect):
		return decodeBrowserSelect(path, block)
	case string(model.BrowserActionCheck):
		return decodeBrowserSelectorOnly(path, block, model.BrowserActionCheck)
	case string(model.BrowserActionUncheck):
		return decodeBrowserSelectorOnly(path, block, model.BrowserActionUncheck)
	case string(model.BrowserActionReload):
		return decodeBrowserNoArg(path, block, model.BrowserActionReload)
	case string(model.BrowserActionBack):
		return decodeBrowserNoArg(path, block, model.BrowserActionBack)
	case string(model.BrowserActionForward):
		return decodeBrowserNoArg(path, block, model.BrowserActionForward)
	}

	blockRange := block.DefRange()

	return nil, hcl.Diagnostics{diagError(
		"Unknown action",
		fmt.Sprintf("action %q is not supported; use goto, click, fill, clear, press, submit, scroll, wait_visible, wait_not_visible, hover, select, check, uncheck, reload, back, or forward.", block.Type),
		&blockRange,
	)}
}

func decodeBrowserGoto(path string, block *hclsyntax.Block) (*model.BrowserAction, hcl.Diagnostics) {
	diags := make(hcl.Diagnostics, 0)

	urlExpr, urlDiags := requireActionAttr(block, "goto", browserURLAttr)
	diags = append(diags, urlDiags...)

	var timeoutExpr, intervalExpr hcl.Expression

	for name, attr := range block.Body.Attributes {
		switch name {
		case browserURLAttr:
			continue
		case browserTimeoutAttr:
			timeoutExpr = attr.Expr
		case browserIntervalAttr:
			intervalExpr = attr.Expr
		default:
			rng := attr.Range()
			diags = append(diags, diagError(
				"Unknown goto attribute",
				fmt.Sprintf("goto attribute %q is not supported; allowed: url, timeout, interval.", name),
				&rng,
			))
		}
	}

	return &model.BrowserAction{
		Kind:     model.BrowserActionGoto,
		File:     path,
		Line:     block.DefRange().Start.Line,
		URL:      expr(path, urlExpr),
		Timeout:  expr(path, timeoutExpr),
		Interval: expr(path, intervalExpr),
	}, diags
}

// decodeBrowserSelectorOnly decodes actions that take only a selector plus
// the optional timeout / interval (click, clear, submit, wait_*, hover,
// check, uncheck).
func decodeBrowserSelectorOnly(path string, block *hclsyntax.Block, kind model.BrowserActionKind) (*model.BrowserAction, hcl.Diagnostics) {
	diags := make(hcl.Diagnostics, 0)
	actionName := string(kind)

	selectorExpr, sDiags := requireActionAttr(block, actionName, browserSelectorAttr)
	diags = append(diags, sDiags...)

	var timeoutExpr, intervalExpr hcl.Expression

	for name, attr := range block.Body.Attributes {
		switch name {
		case browserSelectorAttr:
			continue
		case browserTimeoutAttr:
			timeoutExpr = attr.Expr
		case browserIntervalAttr:
			intervalExpr = attr.Expr
		default:
			rng := attr.Range()
			diags = append(diags, diagError(
				"Unknown "+actionName+" attribute",
				fmt.Sprintf("%s attribute %q is not supported; allowed: selector, timeout, interval.", actionName, name),
				&rng,
			))
		}
	}

	return &model.BrowserAction{
		Kind:     kind,
		File:     path,
		Line:     block.DefRange().Start.Line,
		Selector: expr(path, selectorExpr),
		Timeout:  expr(path, timeoutExpr),
		Interval: expr(path, intervalExpr),
	}, diags
}

func decodeBrowserFill(path string, block *hclsyntax.Block) (*model.BrowserAction, hcl.Diagnostics) {
	diags := make(hcl.Diagnostics, 0)

	selectorExpr, sDiags := requireActionAttr(block, "fill", browserSelectorAttr)
	diags = append(diags, sDiags...)

	valueExpr, vDiags := requireActionAttr(block, "fill", browserValueAttr)
	diags = append(diags, vDiags...)

	var secureExpr, timeoutExpr, intervalExpr hcl.Expression

	for name, attr := range block.Body.Attributes {
		switch name {
		case browserSelectorAttr, browserValueAttr:
			continue
		case browserSecureAttr:
			secureExpr = attr.Expr
		case browserTimeoutAttr:
			timeoutExpr = attr.Expr
		case browserIntervalAttr:
			intervalExpr = attr.Expr
		default:
			rng := attr.Range()
			diags = append(diags, diagError(
				"Unknown fill attribute",
				fmt.Sprintf("fill attribute %q is not supported; allowed: selector, value, secure, timeout, interval.", name),
				&rng,
			))
		}
	}

	return &model.BrowserAction{
		Kind:     model.BrowserActionFill,
		File:     path,
		Line:     block.DefRange().Start.Line,
		Selector: expr(path, selectorExpr),
		Value:    expr(path, valueExpr),
		Secure:   expr(path, secureExpr),
		Timeout:  expr(path, timeoutExpr),
		Interval: expr(path, intervalExpr),
	}, diags
}

func decodeBrowserPress(path string, block *hclsyntax.Block) (*model.BrowserAction, hcl.Diagnostics) {
	diags := make(hcl.Diagnostics, 0)

	keyExpr, kDiags := requireActionAttr(block, "press", browserKeyAttr)
	diags = append(diags, kDiags...)

	var selectorExpr, timeoutExpr, intervalExpr hcl.Expression

	for name, attr := range block.Body.Attributes {
		switch name {
		case browserKeyAttr:
			continue
		case browserSelectorAttr:
			selectorExpr = attr.Expr
		case browserTimeoutAttr:
			timeoutExpr = attr.Expr
		case browserIntervalAttr:
			intervalExpr = attr.Expr
		default:
			rng := attr.Range()
			diags = append(diags, diagError(
				"Unknown press attribute",
				fmt.Sprintf("press attribute %q is not supported; allowed: key, selector, timeout, interval.", name),
				&rng,
			))
		}
	}

	return &model.BrowserAction{
		Kind:     model.BrowserActionPress,
		File:     path,
		Line:     block.DefRange().Start.Line,
		Selector: expr(path, selectorExpr),
		Key:      expr(path, keyExpr),
		Timeout:  expr(path, timeoutExpr),
		Interval: expr(path, intervalExpr),
	}, diags
}

func decodeBrowserSelect(path string, block *hclsyntax.Block) (*model.BrowserAction, hcl.Diagnostics) {
	diags := make(hcl.Diagnostics, 0)

	selectorExpr, sDiags := requireActionAttr(block, "select", browserSelectorAttr)
	diags = append(diags, sDiags...)

	valueExpr, vDiags := requireActionAttr(block, "select", browserValueAttr)
	diags = append(diags, vDiags...)

	var timeoutExpr, intervalExpr hcl.Expression

	for name, attr := range block.Body.Attributes {
		switch name {
		case browserSelectorAttr, browserValueAttr:
			continue
		case browserTimeoutAttr:
			timeoutExpr = attr.Expr
		case browserIntervalAttr:
			intervalExpr = attr.Expr
		default:
			rng := attr.Range()
			diags = append(diags, diagError(
				"Unknown select attribute",
				fmt.Sprintf("select attribute %q is not supported; allowed: selector, value, timeout, interval.", name),
				&rng,
			))
		}
	}

	return &model.BrowserAction{
		Kind:     model.BrowserActionSelect,
		File:     path,
		Line:     block.DefRange().Start.Line,
		Selector: expr(path, selectorExpr),
		Value:    expr(path, valueExpr),
		Timeout:  expr(path, timeoutExpr),
		Interval: expr(path, intervalExpr),
	}, diags
}

// decodeBrowserScroll accepts two mutually exclusive forms:
//   - selector form: scrolls the element matching selector into view
//   - offset form:   scrolls the page by x / y pixels
func decodeBrowserScroll(path string, block *hclsyntax.Block) (*model.BrowserAction, hcl.Diagnostics) {
	diags := make(hcl.Diagnostics, 0)

	var (
		selectorExpr, xExpr, yExpr hcl.Expression
		timeoutExpr, intervalExpr  hcl.Expression
		hasSelector, hasX, hasY    bool
		selectorRange, xRange      hcl.Range
	)

	for name, attr := range block.Body.Attributes {
		switch name {
		case browserSelectorAttr:
			selectorExpr = attr.Expr
			hasSelector = true
			selectorRange = attr.Range()
		case "x":
			xExpr = attr.Expr
			hasX = true
			xRange = attr.Range()
		case "y":
			yExpr = attr.Expr
			hasY = true
		case browserTimeoutAttr:
			timeoutExpr = attr.Expr
		case browserIntervalAttr:
			intervalExpr = attr.Expr
		default:
			rng := attr.Range()
			diags = append(diags, diagError(
				"Unknown scroll attribute",
				fmt.Sprintf("scroll attribute %q is not supported; allowed: selector, x, y, timeout, interval.", name),
				&rng,
			))
		}
	}

	switch {
	case hasSelector && (hasX || hasY):
		diags = append(diags, diagError(
			"Conflicting scroll attributes",
			"scroll must use either selector or x/y offsets, not both.",
			&selectorRange,
		))
	case !hasSelector && !hasX && !hasY:
		blockRange := block.DefRange()
		diags = append(diags, diagError(
			"Missing scroll target",
			"scroll must declare either selector or x/y offsets.",
			&blockRange,
		))
	case !hasSelector && (!hasX || !hasY):
		diags = append(diags, diagError(
			"Incomplete scroll offsets",
			"scroll offset form requires both x and y.",
			&xRange,
		))
	}

	return &model.BrowserAction{
		Kind:     model.BrowserActionScroll,
		File:     path,
		Line:     block.DefRange().Start.Line,
		Selector: expr(path, selectorExpr),
		X:        expr(path, xExpr),
		Y:        expr(path, yExpr),
		Timeout:  expr(path, timeoutExpr),
		Interval: expr(path, intervalExpr),
	}, diags
}

// decodeBrowserNoArg decodes actions that take no attributes (reload, back,
// forward).
func decodeBrowserNoArg(path string, block *hclsyntax.Block, kind model.BrowserActionKind) (*model.BrowserAction, hcl.Diagnostics) {
	diags := make(hcl.Diagnostics, 0)
	actionName := string(kind)

	var timeoutExpr, intervalExpr hcl.Expression

	for name, attr := range block.Body.Attributes {
		switch name {
		case browserTimeoutAttr:
			timeoutExpr = attr.Expr
		case browserIntervalAttr:
			intervalExpr = attr.Expr
		default:
			rng := attr.Range()
			diags = append(diags, diagError(
				"Unknown "+actionName+" attribute",
				fmt.Sprintf("%s attribute %q is not supported; allowed: timeout, interval.", actionName, name),
				&rng,
			))
		}
	}

	return &model.BrowserAction{
		Kind:     kind,
		File:     path,
		Line:     block.DefRange().Start.Line,
		Timeout:  expr(path, timeoutExpr),
		Interval: expr(path, intervalExpr),
	}, diags
}

// decodeBrowserExpect converts the shared expect block into a
// model.BrowserExpect, rejecting any mobile-style id attribute.
func decodeBrowserExpect(path string, expect *expectBlock, out *model.BrowserExpect) hcl.Diagnostics {
	expected := len(expect.Visible) + len(expect.NotVisible) + len(expect.Text) +
		len(expect.Value) + len(expect.Enabled) + len(expect.Disabled) +
		len(expect.Attribute) + len(expect.URL) + len(expect.Title)
	diags := make(hcl.Diagnostics, 0, expected)

	for _, v := range expect.Visible {
		diags = append(diags, rejectMobileID(v.ID, "visible")...)

		out.Visible = append(out.Visible, browserVisibilityFromBlock(path, v))
	}

	for _, v := range expect.NotVisible {
		diags = append(diags, rejectMobileID(v.ID, "not_visible")...)

		out.NotVisible = append(out.NotVisible, browserVisibilityFromBlock(path, v))
	}

	for _, v := range expect.Text {
		diags = append(diags, rejectMobileID(v.ID, "text")...)

		out.Text = append(out.Text, browserValueExpectationFromBlock(path, v))
	}

	for _, v := range expect.Value {
		diags = append(diags, rejectMobileID(v.ID, "value")...)

		out.Value = append(out.Value, browserValueExpectationFromBlock(path, v))
	}

	for _, v := range expect.Enabled {
		diags = append(diags, rejectMobileID(v.ID, "enabled")...)

		out.Enabled = append(out.Enabled, browserStateExpectationFromBlock(path, v))
	}

	for _, v := range expect.Disabled {
		diags = append(diags, rejectMobileID(v.ID, "disabled")...)

		out.Disabled = append(out.Disabled, browserStateExpectationFromBlock(path, v))
	}

	for _, v := range expect.Attribute {
		if v == nil {
			continue
		}

		out.Attribute = append(out.Attribute, model.BrowserAttributeExpectation{
			Selector: expr(path, v.Selector),
			Name:     expr(path, v.Name),
			Expected: expr(path, v.Value),
			Timeout:  expr(path, v.Timeout),
			Interval: expr(path, v.Interval),
		})
	}

	for _, v := range expect.URL {
		if v == nil {
			continue
		}

		out.URL = append(out.URL, model.BrowserURLExpectation{
			Expected: expr(path, v.Value),
			Timeout:  expr(path, v.Timeout),
			Interval: expr(path, v.Interval),
		})
	}

	for _, v := range expect.Title {
		if v == nil {
			continue
		}

		out.Title = append(out.Title, model.BrowserTitleExpectation{
			Expected: expr(path, v.Value),
			Timeout:  expr(path, v.Timeout),
			Interval: expr(path, v.Interval),
		})
	}

	diags = append(diags, decodeWebPerfBlocks(path, expect.WebPerf, out)...)

	return diags
}

// rejectMobileID emits a diag when a browser expect block uses the
// mobile-style "id" attribute. Browser locators are CSS selectors.
func rejectMobileID(id hcl.Expression, blockName string) hcl.Diagnostics {
	if !exprIsSet(id) {
		return nil
	}

	rng := id.Range()

	return hcl.Diagnostics{diagError(
		"Unexpected id attribute",
		fmt.Sprintf("browser %s block uses selector (CSS), not id. Did you mean to use provider \"mobile\"?", blockName),
		&rng,
	)}
}

func browserVisibilityFromBlock(path string, v *visibleBlock) model.BrowserVisibility {
	if v == nil {
		return model.BrowserVisibility{}
	}

	return model.BrowserVisibility{
		Selector: expr(path, v.Selector),
		Timeout:  expr(path, v.Timeout),
		Interval: expr(path, v.Interval),
	}
}

func browserValueExpectationFromBlock(path string, v *valueBlock) model.BrowserValueExpectation {
	if v == nil {
		return model.BrowserValueExpectation{}
	}

	return model.BrowserValueExpectation{
		Selector: expr(path, v.Selector),
		Expected: expr(path, v.Value),
		Timeout:  expr(path, v.Timeout),
		Interval: expr(path, v.Interval),
	}
}

func browserStateExpectationFromBlock(path string, v *stateBlock) model.BrowserStateExpectation {
	if v == nil {
		return model.BrowserStateExpectation{}
	}

	return model.BrowserStateExpectation{
		Selector: expr(path, v.Selector),
		Timeout:  expr(path, v.Timeout),
		Interval: expr(path, v.Interval),
	}
}
