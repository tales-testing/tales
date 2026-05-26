package parser

import (
	"fmt"
	"sort"
	"time"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/hyperxlab/tales/internal/model"
	"github.com/zclconf/go-cty/cty"
)

func decodeFile(path string, body hcl.Body) (*model.Suite, hcl.Diagnostics) {
	var raw fileSchema

	diags := gohcl.DecodeBody(body, nil, &raw)
	if diags.HasErrors() {
		return nil, diags
	}

	// syntaxBody lets us recover the textual order of interleaved step/case
	// blocks, which gohcl loses by decoding them into separate slices. It is
	// nil for non-native bodies, in which case reordering falls back to a
	// no-op (.tales files are always HCL native syntax).
	syntaxBody, _ := body.(*hclsyntax.Body)

	suite := &model.Suite{
		Version:    raw.Version,
		Files:      []string{path},
		ConfigExpr: map[string]model.Expression{},
		Generators: map[string]*model.Generator{},
		Keywords:   map[string]*model.Keyword{},
	}

	for _, cfg := range raw.Config {
		attrs, attrDiags := cfg.Body.JustAttributes()
		diags = append(diags, attrDiags...)

		for name, attr := range attrs {
			suite.ConfigExpr[name] = model.Expression{Expr: attr.Expr, File: path, Line: attr.Range.Start.Line}
		}
	}

	for _, gen := range raw.Generators {
		params, pDiags := bodyToNamedExprMap(path, gen.Body)
		diags = append(diags, pDiags...)
		suite.Generators[gen.Name] = &model.Generator{
			Type:   gen.Type,
			Name:   gen.Name,
			File:   path,
			Params: params,
		}
	}

	keywordBlocks := childBlocks(syntaxBody, "keyword")

	for i, kw := range raw.Keywords {
		inputs := map[string]model.Expression{}

		if kw.InputsBlock != nil {
			var iDiags hcl.Diagnostics

			inputs, iDiags = bodyToNamedExprMap(path, kw.InputsBlock.Body)
			diags = append(diags, iDiags...)
		}

		outputs := map[string]model.Expression{}

		if kw.Outputs != nil {
			var oDiags hcl.Diagnostics

			outputs, oDiags = bodyToNamedExprMap(path, kw.Outputs.Body)
			diags = append(diags, oDiags...)
		}

		steps, sDiags := decodeSteps(path, append(kw.Steps, kw.Cases...))
		diags = append(diags, sDiags...)

		steps = reorderStepsBySource(steps, sourceOrder(blockBodyAt(keywordBlocks, i)))

		suite.Keywords[kw.Name] = &model.Keyword{
			Name:    kw.Name,
			File:    path,
			Inputs:  inputs,
			Steps:   steps,
			Outputs: outputs,
		}
	}

	scenarioBlocks := childBlocks(syntaxBody, "scenario")

	for i, sc := range raw.Scenarios {
		normalSteps, sDiags := decodeSteps(path, append(sc.Steps, sc.Cases...))
		diags = append(diags, sDiags...)

		scenarioBody := blockBodyAt(scenarioBlocks, i)
		normalSteps = reorderStepsBySource(normalSteps, sourceOrder(scenarioBody))

		teardownSteps := make([]*model.Step, 0)
		teardownBlocks := childBlocks(scenarioBody, "teardown")

		for j, td := range sc.Teardowns {
			steps, tDiags := decodeSteps(path, append(td.Steps, td.Cases...))
			diags = append(diags, tDiags...)

			steps = reorderStepsBySource(steps, sourceOrder(blockBodyAt(teardownBlocks, j)))
			teardownSteps = append(teardownSteps, steps...)
		}

		skipRules, scSkipDiags := decodeSkipRules(path, sc.SkipIf, sc.SkipUnless)
		diags = append(diags, scSkipDiags...)

		suite.Scenarios = append(suite.Scenarios, &model.Scenario{
			Name:      sc.Name,
			Tags:      sc.Tags,
			File:      path,
			Steps:     normalSteps,
			Teardown:  teardownSteps,
			SkipRules: skipRules,
		})
	}

	return suite, diags
}

func decodeSteps(path string, rawSteps []stepBlock) ([]*model.Step, hcl.Diagnostics) {
	diags := make(hcl.Diagnostics, 0)
	steps := make([]*model.Step, 0, len(rawSteps))

	for _, rs := range rawSteps {
		when := model.Expression{}
		if exprIsSet(rs.When) {
			when = expr(path, rs.When)
		}

		step := &model.Step{
			Provider:  rs.Provider,
			Name:      rs.Name,
			File:      path,
			DependsOn: append([]string(nil), rs.DependsOn...),
			When:      when,
			Capture:   map[string]model.Expression{},
		}
		if rs.When != nil {
			step.Line = rs.When.Range().Start.Line
		}

		if step.Line == 0 {
			step.Line = 1
		}

		if rs.Request != nil {
			auth, authDiags := decodeRequestAuth(path, rs.Request.Auth)
			diags = append(diags, authDiags...)
			body, bodyDiags := decodeRequestBody(path, rs.Request.Body)
			diags = append(diags, bodyDiags...)

			step.Request = &model.Request{
				Method:  expr(path, rs.Request.Method),
				URL:     expr(path, rs.Request.URL),
				Headers: expr(path, rs.Request.Headers),
				Query:   expr(path, rs.Request.Query),
				Body:    body,
				Timeout: expr(path, rs.Request.Timeout),
				Auth:    auth,
			}
		}

		expect := rs.Expect
		if expect == nil {
			expect = rs.Response
		}

		if expect != nil {
			step.Expect = &model.Expect{
				Status:  expr(path, expect.Status),
				Headers: expr(path, expect.Headers),
				JSON:    expr(path, expect.JSON),
				Body:    expr(path, expect.Body),
				Strict:  expr(path, expect.Strict),
			}
		}

		if rs.Retry != nil {
			retry, rDiags := decodeRetry(rs.Retry)
			diags = append(diags, rDiags...)
			step.Retry = retry
		}

		if rs.Capture != nil {
			capture, cDiags := bodyToNamedExprMap(path, rs.Capture.Body)
			diags = append(diags, cDiags...)
			step.Capture = capture
		}

		if rs.Vars != nil {
			vars, vDiags := decodeStepVars(path, rs.Vars)
			diags = append(diags, vDiags...)
			step.Vars = vars
		}

		if rs.Provider == "keyword" {
			step.Keyword = &model.KeywordCall{
				Name:   expr(path, rs.CallName),
				Inputs: expr(path, rs.Inputs),
			}
		}

		mobileStep, mobileDiags := decodeMobileStepIfNeeded(path, rs, step.Name)
		diags = append(diags, mobileDiags...)

		if mobileStep != nil {
			step.Mobile = mobileStep
		}

		sqlStep, sqlDiags := decodeSQLStepIfNeeded(path, rs, step.Name)
		diags = append(diags, sqlDiags...)

		if sqlStep != nil {
			step.SQL = sqlStep
		}

		if step.Provider == "" {
			diags = append(diags, &hcl.Diagnostic{
				Severity: hcl.DiagError,
				Summary:  "Missing step provider",
				Detail:   fmt.Sprintf("Step %q has no provider label.", step.Name),
			})
		}

		skipRules, skipDiags := decodeSkipRules(path, rs.SkipIf, rs.SkipUnless)
		diags = append(diags, skipDiags...)
		step.SkipRules = skipRules

		steps = append(steps, step)
	}

	return steps, diags
}

func decodeMobileStepIfNeeded(path string, rs stepBlock, stepName string) (*model.MobileStep, hcl.Diagnostics) {
	if rs.Provider == mobileProviderType {
		return decodeMobileStep(path, rs)
	}

	if !looksLikeMobileStep(rs) {
		return nil, nil
	}

	return nil, hcl.Diagnostics{diagError(
		"Mobile fields on non-mobile step",
		fmt.Sprintf("Step %q uses mobile-only fields (platform, target, launch, terminate, actions, or mobile expectations) but its provider is %q; use provider \"mobile\".", stepName, rs.Provider),
		nil,
	)}
}

func decodeRequestBody(path string, raw []bodyBlock) (*model.RequestBody, hcl.Diagnostics) {
	diags := make(hcl.Diagnostics, 0, 2)
	if len(raw) == 0 {
		return nil, diags
	}

	if len(raw) > 1 {
		diags = append(diags, diagError("Duplicate request body block", "request supports at most one body block.", nil))
	}

	first := raw[0]

	count := 0

	var firstRange *hcl.Range

	if exprIsSet(first.JSON) {
		count++

		valueRange := first.JSON.Range()
		firstRange = &valueRange
	}

	if exprIsSet(first.Form) {
		count++

		valueRange := first.Form.Range()
		if firstRange == nil {
			firstRange = &valueRange
		}
	}

	if exprIsSet(first.Raw) {
		count++

		valueRange := first.Raw.Range()
		if firstRange == nil {
			firstRange = &valueRange
		}
	}

	var multipart *model.MultipartBody

	if first.Multipart != nil {
		count++

		mp, mpDiags := decodeMultipartBlock(path, first.Multipart)
		diags = append(diags, mpDiags...)
		multipart = mp
	}

	if count == 0 {
		diags = append(diags, diagError("Missing request body content", "body block must define exactly one of json, form, raw, or a multipart block.", nil))
	}

	if count > 1 {
		diags = append(diags, diagError("Conflicting request body fields", "body block must define exactly one of json, form, raw, or a multipart block.", firstRange))
	}

	return &model.RequestBody{
		JSON:      expr(path, first.JSON),
		Form:      expr(path, first.Form),
		Raw:       expr(path, first.Raw),
		Multipart: multipart,
	}, diags
}

// decodeMultipartBlock decodes a body { multipart { ... } } block, preserving
// the textual order of file / field children so the serialized wire
// representation is deterministic. Each file block must declare exactly one
// of path / content; each field block must declare both name and value.
//
// gohcl is intentionally not used for the file / field children because its
// slice decoding loses the interleaved declaration order between file and
// field blocks. Instead we walk the underlying hclsyntax.Body and decode
// each child by hand.
func decodeMultipartBlock(path string, raw *multipartBlock) (*model.MultipartBody, hcl.Diagnostics) {
	diags := make(hcl.Diagnostics, 0)

	syntaxBody, ok := raw.Body.(*hclsyntax.Body)
	if !ok {
		bodyRange := raw.Body.MissingItemRange()
		diags = append(diags, diagError(
			"Unsupported multipart body type",
			"multipart blocks require HCL native syntax to preserve part declaration order; non-native bodies (e.g. JSON) are not supported.",
			&bodyRange,
		))

		return nil, diags
	}

	for name, attr := range syntaxBody.Attributes {
		attrRange := attr.Range()
		diags = append(diags, diagError(
			"Unexpected multipart attribute",
			fmt.Sprintf("multipart blocks only accept file { } and field { } children, found attribute %q.", name),
			&attrRange,
		))
	}

	body := &model.MultipartBody{}

	for _, block := range syntaxBody.Blocks {
		switch block.Type {
		case "file":
			part, partDiags := decodeMultipartFile(path, block)
			diags = append(diags, partDiags...)

			if part != nil {
				body.Parts = append(body.Parts, model.MultipartPart{File: part})
			}
		case "field":
			part, partDiags := decodeMultipartField(path, block)
			diags = append(diags, partDiags...)

			if part != nil {
				body.Parts = append(body.Parts, model.MultipartPart{Field: part})
			}
		default:
			blockRange := block.DefRange()
			diags = append(diags, diagError(
				"Unknown multipart child",
				fmt.Sprintf("multipart supports file and field blocks only, found %q.", block.Type),
				&blockRange,
			))
		}
	}

	return body, diags
}

var multipartFileAllowed = map[string]struct{}{
	"field":        {},
	"path":         {},
	"content":      {},
	"filename":     {},
	"content_type": {},
}

func decodeMultipartFile(path string, block *hclsyntax.Block) (*model.MultipartFilePart, hcl.Diagnostics) {
	diags := make(hcl.Diagnostics, 0)

	if len(block.Body.Blocks) > 0 {
		blockRange := block.Body.Blocks[0].DefRange()
		diags = append(diags, diagError(
			"Unexpected multipart.file sub-block",
			fmt.Sprintf("multipart.file accepts attributes only, found block %q.", block.Body.Blocks[0].Type),
			&blockRange,
		))
	}

	for name, attr := range block.Body.Attributes {
		if _, ok := multipartFileAllowed[name]; !ok {
			attrRange := attr.Range()
			diags = append(diags, diagError(
				"Unknown multipart.file attribute",
				fmt.Sprintf("multipart.file attribute %q is not supported. Use field, path, content, filename, or content_type.", name),
				&attrRange,
			))
		}
	}

	attr := block.Body.Attributes

	fieldAttr, hasField := attr["field"]
	if !hasField {
		blockRange := block.DefRange()
		diags = append(diags, diagError(
			"Missing multipart file field",
			"multipart.file requires a field attribute (the form field name).",
			&blockRange,
		))
	}

	pathAttr, hasPath := attr["path"]
	contentAttr, hasContent := attr["content"]

	switch {
	case !hasPath && !hasContent:
		blockRange := block.DefRange()
		diags = append(diags, diagError(
			"Missing multipart file source",
			"multipart.file must declare exactly one of path or content.",
			&blockRange,
		))
	case hasPath && hasContent:
		pathRange := pathAttr.Range()
		diags = append(diags, diagError(
			"Conflicting multipart file source",
			"multipart.file must declare exactly one of path or content, not both.",
			&pathRange,
		))
	}

	return &model.MultipartFilePart{
		Field:       hclsyntaxAttrExpr(path, fieldAttr),
		Path:        hclsyntaxAttrExpr(path, pathAttr),
		Content:     hclsyntaxAttrExpr(path, contentAttr),
		Filename:    hclsyntaxAttrExpr(path, attr["filename"]),
		ContentType: hclsyntaxAttrExpr(path, attr["content_type"]),
	}, diags
}

var multipartFieldAllowed = map[string]struct{}{
	"name":  {},
	"value": {},
}

func decodeMultipartField(path string, block *hclsyntax.Block) (*model.MultipartFieldPart, hcl.Diagnostics) {
	diags := make(hcl.Diagnostics, 0)

	if len(block.Body.Blocks) > 0 {
		blockRange := block.Body.Blocks[0].DefRange()
		diags = append(diags, diagError(
			"Unexpected multipart.field sub-block",
			fmt.Sprintf("multipart.field accepts attributes only, found block %q.", block.Body.Blocks[0].Type),
			&blockRange,
		))
	}

	for name, attr := range block.Body.Attributes {
		if _, ok := multipartFieldAllowed[name]; !ok {
			attrRange := attr.Range()
			diags = append(diags, diagError(
				"Unknown multipart.field attribute",
				fmt.Sprintf("multipart.field attribute %q is not supported. Use name or value.", name),
				&attrRange,
			))
		}
	}

	attr := block.Body.Attributes

	nameAttr, hasName := attr["name"]
	if !hasName {
		blockRange := block.DefRange()
		diags = append(diags, diagError(
			"Missing multipart field name",
			"multipart.field requires a name attribute.",
			&blockRange,
		))
	}

	valueAttr, hasValue := attr["value"]
	if !hasValue {
		blockRange := block.DefRange()
		diags = append(diags, diagError(
			"Missing multipart field value",
			"multipart.field requires a value attribute.",
			&blockRange,
		))
	}

	return &model.MultipartFieldPart{
		Name:  hclsyntaxAttrExpr(path, nameAttr),
		Value: hclsyntaxAttrExpr(path, valueAttr),
	}, diags
}

func hclsyntaxAttrExpr(path string, attr *hclsyntax.Attribute) model.Expression {
	if attr == nil {
		return model.Expression{}
	}

	return model.Expression{Expr: attr.Expr, File: path, Line: attr.Range().Start.Line}
}

func decodeRequestAuth(path string, raw []authBlock) (*model.RequestAuth, hcl.Diagnostics) {
	diags := make(hcl.Diagnostics, 0)
	if len(raw) == 0 {
		return nil, diags
	}

	if len(raw) > 1 {
		diags = append(diags, diagError("Duplicate auth block", "request supports at most one auth block.", nil))
	}

	auth := &model.RequestAuth{}
	first := raw[0]

	if len(first.Basic) == 0 {
		diags = append(diags, diagError("Missing auth scheme", "auth block must contain a basic block.", nil))

		return auth, diags
	}

	if len(first.Basic) > 1 {
		diags = append(diags, diagError("Duplicate basic auth block", "auth supports at most one basic block.", nil))
	}

	attrs, attrDiags := first.Basic[0].Body.JustAttributes()
	diags = append(diags, attrDiags...)

	usernameAttr, hasUsername := attrs["username"]
	if !hasUsername {
		diags = append(diags, diagError("Missing basic auth username", "auth.basic.username is required.", nil))
	}

	passwordAttr, hasPassword := attrs["password"]
	if !hasPassword {
		diags = append(diags, diagError("Missing basic auth password", "auth.basic.password is required.", nil))
	}

	auth.Basic = &model.BasicAuth{
		Username: attrExpr(path, usernameAttr),
		Password: attrExpr(path, passwordAttr),
	}

	return auth, diags
}

func attrExpr(path string, attr *hcl.Attribute) model.Expression {
	if attr == nil {
		return model.Expression{}
	}

	return model.Expression{Expr: attr.Expr, File: path, Line: attr.Range.Start.Line}
}

func decodeRetry(raw *retryBlock) (*model.Retry, hcl.Diagnostics) {
	retry := &model.Retry{Attempts: 1}
	diags := make(hcl.Diagnostics, 0, 2)

	if raw.Attempts == nil && raw.Interval == nil {
		return retry, diags
	}

	attempts, attemptsDiags := decodeRetryAttempts(raw.Attempts)

	diags = append(diags, attemptsDiags...)
	if !attemptsDiags.HasErrors() {
		retry.Attempts = attempts
	}

	interval, intervalDiags := decodeRetryInterval(raw.Interval)

	diags = append(diags, intervalDiags...)
	if !intervalDiags.HasErrors() {
		retry.Interval = interval
	}

	return retry, diags
}

func decodeRetryAttempts(expression hcl.Expression) (int, hcl.Diagnostics) {
	if expression == nil {
		return 1, nil
	}

	value, diags := expression.Value(nil)
	if diags.HasErrors() {
		return 1, diags
	}

	attempts, err := numberToInt(value)
	if err != nil {
		rangeValue := expression.Range()

		return 1, hcl.Diagnostics{diagError("Invalid retry attempts", err.Error(), &rangeValue)}
	}

	if attempts < 1 {
		rangeValue := expression.Range()

		return 1, hcl.Diagnostics{diagError("Invalid retry attempts", "retry.attempts must be greater than or equal to 1.", &rangeValue)}
	}

	return attempts, nil
}

func decodeRetryInterval(expression hcl.Expression) (time.Duration, hcl.Diagnostics) {
	if expression == nil {
		return 0, nil
	}

	value, diags := expression.Value(nil)
	if diags.HasErrors() {
		return 0, diags
	}

	interval, err := stringToDuration(value)
	if err != nil {
		rangeValue := expression.Range()

		return 0, hcl.Diagnostics{diagError("Invalid retry interval", err.Error(), &rangeValue)}
	}

	return interval, nil
}

func numberToInt(value cty.Value) (int, error) {
	if value.Type() != cty.Number {
		return 0, fmt.Errorf("retry.attempts must be a number")
	}

	attempts, accuracy := value.AsBigFloat().Int64()
	if accuracy != 0 {
		return 0, fmt.Errorf("retry.attempts must be an integer")
	}

	return int(attempts), nil
}

func stringToDuration(value cty.Value) (time.Duration, error) {
	if value.Type() != cty.String {
		return 0, fmt.Errorf("retry.interval must be a duration string")
	}

	interval, err := time.ParseDuration(value.AsString())
	if err != nil {
		return 0, fmt.Errorf("retry.interval must be a valid duration: %w", err)
	}

	return interval, nil
}

// decodeStepVars decodes a vars block while preserving the textual
// declaration order of its attributes. The runtime relies on this order so
// that later vars can read earlier ones via vars.<name>.
//
// .tales files always parse to a *hclsyntax.Body (HCL native syntax). Any
// other body type cannot expose attribute source ordering, so rather than
// silently sorting alphabetically — which would break the documented
// declaration-order contract — we emit a diagnostic and refuse to decode.
func decodeStepVars(path string, raw *varsBlock) ([]model.StepVar, hcl.Diagnostics) {
	if raw == nil {
		return nil, nil
	}

	diags := make(hcl.Diagnostics, 0)

	syntaxBody, ok := raw.Body.(*hclsyntax.Body)
	if !ok {
		bodyRange := raw.Body.MissingItemRange()
		diags = append(diags, diagError(
			"Unsupported vars body type",
			"vars blocks require HCL native syntax to preserve attribute declaration order; non-native bodies (e.g. JSON) are not supported.",
			&bodyRange,
		))

		return nil, diags
	}

	for _, block := range syntaxBody.Blocks {
		blockRange := block.DefRange()
		diags = append(diags, diagError(
			"Unexpected vars sub-block",
			fmt.Sprintf("vars supports attributes only, found block %q.", block.Type),
			&blockRange,
		))
	}

	items := make([]*hclsyntax.Attribute, 0, len(syntaxBody.Attributes))
	for _, attr := range syntaxBody.Attributes {
		items = append(items, attr)
	}

	sort.SliceStable(items, func(i, j int) bool {
		return items[i].Range().Start.Byte < items[j].Range().Start.Byte
	})

	result := make([]model.StepVar, 0, len(items))

	for _, attr := range items {
		result = append(result, model.StepVar{
			Name: attr.Name,
			Expr: model.Expression{Expr: attr.Expr, File: path, Line: attr.Range().Start.Line},
		})
	}

	return result, diags
}

func bodyToNamedExprMap(path string, body hcl.Body) (map[string]model.Expression, hcl.Diagnostics) {
	attrs, diags := body.JustAttributes()

	res := make(map[string]model.Expression, len(attrs))
	for name, attr := range attrs {
		res[name] = model.Expression{Expr: attr.Expr, File: path, Line: attr.Range.Start.Line}
	}

	return res, diags
}

func expr(path string, e hcl.Expression) model.Expression {
	if e == nil {
		return model.Expression{}
	}

	return model.Expression{Expr: e, File: path, Line: e.Range().Start.Line}
}
