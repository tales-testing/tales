package lang

import (
	"fmt"
	"slices"

	"github.com/hashicorp/hcl/v2"
	"github.com/tales-testing/tales/internal/model"
)

// FindStepRefs returns referenced result.<step> names from expression.
func FindStepRefs(expr hcl.Expression) []string {
	return findRootAttrRefs(expr, "result")
}

// FindVarRefs returns referenced vars.<name> names from expression.
func FindVarRefs(expr hcl.Expression) []string {
	return findRootAttrRefs(expr, "vars")
}

// FindFileRefs returns the attribute names referenced under the file.<attr>
// namespace (path, exists, size_bytes, sha256, text, json, …). The file
// provider uses it to decide which reads a capture expression requires.
func FindFileRefs(expr hcl.Expression) []string {
	return findRootAttrRefs(expr, "file")
}

func findRootAttrRefs(expr hcl.Expression, rootName string) []string {
	if expr == nil {
		return nil
	}

	refs := map[string]struct{}{}

	for _, traversal := range expr.Variables() {
		if len(traversal) < 2 {
			continue
		}

		root, ok := traversal[0].(hcl.TraverseRoot)
		if !ok || root.Name != rootName {
			continue
		}

		attr, ok := traversal[1].(hcl.TraverseAttr)
		if !ok {
			continue
		}

		refs[attr.Name] = struct{}{}
	}

	out := make([]string, 0, len(refs))
	for ref := range refs {
		out = append(out, ref)
	}

	return out
}

// StepDependencies builds dependency set for one step from explicit and implicit references.
func StepDependencies(step *model.Step) (map[string]struct{}, error) {
	deps := map[string]struct{}{}
	for _, dep := range step.DependsOn {
		deps[dep] = struct{}{}
	}

	collect := func(expression model.Expression) {
		for _, dep := range FindStepRefs(expression.Expr) {
			deps[dep] = struct{}{}
		}
	}

	if step.When.Expr != nil {
		collect(step.When)
	}

	for _, v := range step.Vars {
		collect(v.Expr)
	}

	collectRequestRefs(step.Request, collect)
	collectExpectRefs(step.Expect, collect)

	for _, capExpr := range step.Capture {
		collect(capExpr)
	}

	collectSaveRefs(step.Save, collect)
	collectFileRefs(step.FileOp, collect)
	collectExecRefs(step.Exec, collect)

	if step.Keyword != nil {
		collect(step.Keyword.Name)
		collect(step.Keyword.Inputs)
	}

	collectMobileRefs(step.Mobile, collect)
	collectSQLRefs(step.SQL, collect)
	collectWebhookRefs(step.Webhook, collect)
	collectRPCRefs(step.RPC, collect)
	collectSkipRefs(step.SkipRules, collect)

	// A step referencing its own name — through depends_on or a
	// result.<self> expression — is always invalid: its result does not
	// exist yet while it runs.
	if _, selfRef := deps[step.Name]; selfRef {
		return nil, fmt.Errorf("step %q cannot depend on itself", step.Name)
	}

	return deps, nil
}

// ValidateStepOrder verifies that every step references only steps defined
// earlier in file order, through either depends_on or result.<x> expressions.
// steps must be in .tales source order. externalDeps holds names resolvable
// outside the list (for example results injected into a keyword by its
// caller); they never trigger a forward-reference error.
func ValidateStepOrder(steps []*model.Step, externalDeps map[string]struct{}) error {
	known := make(map[string]struct{}, len(steps))
	for _, step := range steps {
		known[step.Name] = struct{}{}
	}

	seen := make(map[string]struct{}, len(steps))

	for _, step := range steps {
		if err := validateStepRefs(step, seen, known, externalDeps); err != nil {
			return err
		}

		seen[step.Name] = struct{}{}
	}

	return nil
}

// validateStepRefs checks one step's explicit and implicit dependencies
// against the steps already seen, the full set of known step names, and the
// externally resolvable names.
func validateStepRefs(step *model.Step, seen, known, externalDeps map[string]struct{}) error {
	for _, dep := range step.DependsOn {
		if dep == step.Name {
			continue // self-reference is reported by StepDependencies below
		}

		if _, ok := seen[dep]; ok {
			continue
		}

		if _, ok := externalDeps[dep]; ok {
			continue
		}

		if _, ok := known[dep]; ok {
			return fmt.Errorf("step %q depends on %q, but %q is defined later", step.Name, dep, dep)
		}

		return fmt.Errorf("step %q depends on unknown step %q", step.Name, dep)
	}

	deps, err := StepDependencies(step)
	if err != nil {
		return err
	}

	for dep := range deps {
		if slices.Contains(step.DependsOn, dep) {
			continue // already validated in the depends_on loop above
		}

		if _, ok := seen[dep]; ok {
			continue
		}

		if _, ok := externalDeps[dep]; ok {
			continue
		}

		if _, ok := known[dep]; ok {
			return fmt.Errorf("step %q references result.%s, but %q is defined later", step.Name, dep, dep)
		}

		return fmt.Errorf("step %q references unknown dependency %q", step.Name, dep)
	}

	return nil
}

func collectRequestRefs(req *model.Request, collect func(model.Expression)) {
	if req == nil {
		return
	}

	collect(req.Method)
	collect(req.URL)
	collect(req.Headers)
	collect(req.Query)

	if req.Body != nil {
		collect(req.Body.JSON)
		collect(req.Body.Form)
		collect(req.Body.Raw)
	}

	if req.Auth != nil && req.Auth.Basic != nil {
		collect(req.Auth.Basic.Username)
		collect(req.Auth.Basic.Password)
	}

	collect(req.Timeout)
}

func collectExpectRefs(expect *model.Expect, collect func(model.Expression)) {
	if expect == nil {
		return
	}

	collect(expect.Status)
	collect(expect.Headers)
	collect(expect.JSON)
	collect(expect.Body)
	collect(expect.Strict)
}

func collectSaveRefs(save *model.SaveBlock, collect func(model.Expression)) {
	if save == nil {
		return
	}

	collect(save.Body)
}

func collectFileRefs(file *model.FileCall, collect func(model.Expression)) {
	if file == nil {
		return
	}

	collect(file.Path)

	if file.Expect == nil {
		return
	}

	collect(file.Expect.Exists)
	collect(file.Expect.SizeBytes)
	collect(file.Expect.Text)
	collect(file.Expect.JSON)

	for _, h := range file.Expect.Hashes {
		collect(h)
	}
}

func collectExecRefs(exec *model.ExecCall, collect func(model.Expression)) {
	if exec == nil {
		return
	}

	collect(exec.Command)
	collect(exec.Args)
	collect(exec.Env)
	collect(exec.Stdin)
	collect(exec.Timeout)

	if exec.Sandbox != nil {
		collect(exec.Sandbox.Mode)
		collect(exec.Sandbox.Workdir)
		collect(exec.Sandbox.Env)
		collect(exec.Sandbox.Network)
	}

	if exec.Expect != nil {
		collect(exec.Expect.ExitCode)
		collect(exec.Expect.Stdout)
		collect(exec.Expect.Stderr)
		collect(exec.Expect.StdoutJSON)
	}
}

func collectMobileRefs(mob *model.MobileStep, collect func(model.Expression)) {
	if mob == nil {
		return
	}

	collect(mob.Platform)
	collect(mob.Target)

	if mob.Launch != nil {
		collect(mob.Launch.ClearState)
	}

	for _, action := range mob.Actions {
		collect(action.ID)
		collect(action.Label)
		collect(action.Value)
		collect(action.Secure)
		collect(action.Timeout)
		collect(action.Interval)
		collect(action.Direction)
		collect(action.Distance)
		collect(action.Duration)
	}

	for _, permission := range mob.Permissions {
		collect(permission.Decision)
	}

	collectMobileExpectRefs(mob.Expect, collect)
}

func collectMobileExpectRefs(expect model.MobileExpect, collect func(model.Expression)) {
	for _, v := range expect.Visible {
		collectMobileLocatorRefs(v.ID, v.Label, v.Timeout, v.Interval, collect)
	}

	for _, v := range expect.NotVisible {
		collectMobileLocatorRefs(v.ID, v.Label, v.Timeout, v.Interval, collect)
	}

	for _, v := range expect.Text {
		collectMobileLocatorRefs(v.ID, v.Label, v.Timeout, v.Interval, collect)
		collect(v.Expected)
	}

	for _, v := range expect.Value {
		collectMobileLocatorRefs(v.ID, v.Label, v.Timeout, v.Interval, collect)
		collect(v.Expected)
	}

	for _, v := range expect.Enabled {
		collectMobileLocatorRefs(v.ID, v.Label, v.Timeout, v.Interval, collect)
	}

	for _, v := range expect.Disabled {
		collectMobileLocatorRefs(v.ID, v.Label, v.Timeout, v.Interval, collect)
	}
}

// collectMobileLocatorRefs walks the attributes every mobile expect block
// shares. Factored out because the six blocks repeated the same triple
// verbatim, which is how Label came to be missing from all of them at
// once: a locator field added to the model has to be threaded through six
// identical loops, and was not. Adding one here now covers every block.
func collectMobileLocatorRefs(id, label, timeout, interval model.Expression, collect func(model.Expression)) {
	collect(id)
	collect(label)
	collect(timeout)
	collect(interval)
}

func collectSQLRefs(sql *model.SQLCall, collect func(model.Expression)) {
	if sql == nil {
		return
	}

	collect(sql.Connection)

	if sql.Exec != nil {
		collect(sql.Exec.SQL)
		collect(sql.Exec.Args)
	}

	if sql.Query != nil {
		collect(sql.Query.SQL)
		collect(sql.Query.Args)
	}
}

func collectWebhookRefs(webhook *model.WebhookCall, collect func(model.Expression)) {
	if webhook == nil {
		return
	}

	collect(webhook.Target)

	if webhook.Start != nil {
		collect(webhook.Start.Address)
		collect(webhook.Start.Path)
		collect(webhook.Start.PublicURL)
		collect(webhook.Start.PublicScheme)
		collect(webhook.Start.PublicHost)
		collect(webhook.Start.PublicPort)
		collect(webhook.Start.MaxBodySize)
	}

	if webhook.Wait != nil {
		collect(webhook.Wait.Timeout)
		collect(webhook.Wait.Count)
	}

	if webhook.Stop != nil {
		collect(webhook.Stop.Target)
	}

	collectWebhookExpectRefs(webhook.Expect, collect)
}

func collectWebhookExpectRefs(expect *model.WebhookExpect, collect func(model.Expression)) {
	if expect == nil {
		return
	}

	if expect.Request != nil {
		collect(expect.Request.Method)
		collect(expect.Request.Path)
		collect(expect.Request.Headers)
		collect(expect.Request.Query)
		collect(expect.Request.JSON)
		collect(expect.Request.Body)
	}

	if expect.HMAC != nil {
		collect(expect.HMAC.Header)
		collect(expect.HMAC.Secret)
		collect(expect.HMAC.Algorithm)
		collect(expect.HMAC.Format)
		collect(expect.HMAC.Payload)
		collect(expect.HMAC.TimestampTolerance)
		collect(expect.HMAC.TimestampRequired)
	}
}

func collectRPCRefs(rpc *model.RPCCall, collect func(model.Expression)) {
	if rpc == nil {
		return
	}

	collect(rpc.Target)
	collect(rpc.Service)
	collect(rpc.Method)
	collect(rpc.Message)
	collect(rpc.Headers)
	collect(rpc.Metadata)
	collect(rpc.Timeout)

	if rpc.Expect == nil {
		return
	}

	collect(rpc.Expect.Status)
	collect(rpc.Expect.Error)
	collect(rpc.Expect.Message)
	collect(rpc.Expect.Headers)
	collect(rpc.Expect.Metadata)
	collect(rpc.Expect.Trailers)
}

func collectSkipRefs(rules []model.SkipRule, collect func(model.Expression)) {
	for _, rule := range rules {
		collect(rule.Condition)
		collect(rule.Reason)
		collect(rule.OS)
		collect(rule.Arch)
		collect(rule.EnvSet)
		collect(rule.Env)
	}
}

// ValidateStepVars enforces the contract for step-local vars at load time:
// each var may reference only vars declared earlier in the same block, no
// var name is duplicated, and every vars.<name> consumed by the rest of the
// step (request, expect, capture, etc.) is declared in this step's vars.
// when and skip rules are evaluated before the step body, so they cannot
// reference vars — those usages produce a dedicated error. Cross-step var
// sharing is intentionally not supported — use capture.
func ValidateStepVars(step *model.Step) error {
	declared := make(map[string]struct{}, len(step.Vars))

	for _, v := range step.Vars {
		for _, ref := range FindVarRefs(v.Expr.Expr) {
			if ref == v.Name {
				return fmt.Errorf("step %q variable %q cannot reference itself", step.Name, v.Name)
			}

			if _, ok := declared[ref]; !ok {
				return fmt.Errorf("step %q variable %q references vars.%s before it is defined", step.Name, v.Name, ref)
			}
		}

		if _, dup := declared[v.Name]; dup {
			return fmt.Errorf("duplicate variable %q in step %q", v.Name, step.Name)
		}

		declared[v.Name] = struct{}{}
	}

	if step.When.Expr != nil {
		if refs := FindVarRefs(step.When.Expr); len(refs) > 0 {
			return fmt.Errorf("step %q cannot reference vars.%s in when: when is evaluated before the step body", step.Name, refs[0])
		}
	}

	for _, rule := range step.SkipRules {
		for _, expression := range []model.Expression{rule.Condition, rule.Reason, rule.OS, rule.Arch, rule.EnvSet, rule.Env} {
			if refs := FindVarRefs(expression.Expr); len(refs) > 0 {
				return fmt.Errorf("step %q cannot reference vars.%s in skip rules: skip rules are evaluated before the step body", step.Name, refs[0])
			}
		}
	}

	seen := map[string]struct{}{}

	collect := func(expression model.Expression) {
		for _, ref := range FindVarRefs(expression.Expr) {
			seen[ref] = struct{}{}
		}
	}

	collectRequestRefs(step.Request, collect)
	collectExpectRefs(step.Expect, collect)

	for _, capExpr := range step.Capture {
		collect(capExpr)
	}

	if step.Keyword != nil {
		collect(step.Keyword.Name)
		collect(step.Keyword.Inputs)
	}

	collectSaveRefs(step.Save, collect)
	collectFileRefs(step.FileOp, collect)
	collectExecRefs(step.Exec, collect)
	collectMobileRefs(step.Mobile, collect)
	collectSQLRefs(step.SQL, collect)
	collectWebhookRefs(step.Webhook, collect)
	collectRPCRefs(step.RPC, collect)

	for ref := range seen {
		if _, ok := declared[ref]; !ok {
			return fmt.Errorf("step %q references unknown variable vars.%s", step.Name, ref)
		}
	}

	return nil
}
