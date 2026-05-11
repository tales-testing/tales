package lang

import (
	"strings"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/hyperxlab/tales/internal/model"
	"github.com/zclconf/go-cty/cty"
)

func TestRegexFindFullMatch(t *testing.T) {
	t.Parallel()

	value := evalTestExpression(t, `regex_find("Your code is A1B2C3", "[A-Z0-9]{6}")`)
	if value.AsString() != "A1B2C3" {
		t.Fatalf("unexpected match: %s", value.AsString())
	}
}

func TestRegexFindCaptureGroup(t *testing.T) {
	t.Parallel()

	value := evalTestExpression(t, `regex_find("Your code is A1B2C3", "code is ([A-Z0-9]{6})", 1)`)
	if value.AsString() != "A1B2C3" {
		t.Fatalf("unexpected capture group: %s", value.AsString())
	}
}

func TestRegexFindNoMatchError(t *testing.T) {
	t.Parallel()

	_, err := evalTestExpressionError(`regex_find("no code", "[A-Z0-9]{6}")`)
	if err == nil || !strings.Contains(err.Error(), "found no match") {
		t.Fatalf("expected no match error, got %v", err)
	}
}

func TestRegexFindInvalidRegexError(t *testing.T) {
	t.Parallel()

	_, err := evalTestExpressionError(`regex_find("value", "[")`)
	if err == nil || !strings.Contains(err.Error(), "pattern is invalid") {
		t.Fatalf("expected invalid regex error, got %v", err)
	}
}

func TestRegexFindGroupOutOfRangeError(t *testing.T) {
	t.Parallel()

	_, err := evalTestExpressionError(`regex_find("Your code is A1B2C3", "code is ([A-Z0-9]{6})", 2)`)
	if err == nil || !strings.Contains(err.Error(), "out of range") {
		t.Fatalf("expected group out of range error, got %v", err)
	}
}

func TestRegexFindRejectsMultipleGroupIndices(t *testing.T) {
	t.Parallel()

	_, err := evalTestExpressionError(`regex_find("Your code is A1B2C3", "code is ([A-Z0-9]{6})", 1, 2)`)
	if err == nil || !strings.Contains(err.Error(), "at most one capture group index") {
		t.Fatalf("expected multiple group index error, got %v", err)
	}
}

func TestRegexFindConvertsNonStringInput(t *testing.T) {
	t.Parallel()

	value := evalTestExpression(t, `regex_find(123456, "[0-9]+")`)
	if value.AsString() != "123456" {
		t.Fatalf("unexpected converted match: %s", value.AsString())
	}
}

func TestURLEncode(t *testing.T) {
	t.Parallel()

	value := evalTestExpression(t, `url_encode("a&b=c +%#")`)
	if value.AsString() != "a%26b%3Dc+%2B%25%23" {
		t.Fatalf("unexpected encoded value: %s", value.AsString())
	}
}

func evalTestExpression(t *testing.T, src string) cty.Value {
	t.Helper()

	value, err := evalTestExpressionError(src)
	if err != nil {
		t.Fatalf("eval failed: %v", err)
	}

	return value
}

func evalTestExpressionError(src string) (cty.Value, error) {
	evaluator := NewEvaluator(nil)

	return evaluator.Eval(parseLangFunctionExpr(src), ScopeData{
		Config:   map[string]cty.Value{},
		Result:   map[string]cty.Value{},
		Request:  map[string]cty.Value{},
		Response: map[string]cty.Value{},
		Input:    map[string]cty.Value{},
	}, GenerateMeta{})
}

func parseLangFunctionExpr(src string) model.Expression {
	expr, diags := hclsyntax.ParseExpression([]byte(src), "test.hcl", hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		panic(diags.Error())
	}

	return model.Expression{Expr: expr, File: "test.hcl", Line: 1}
}
