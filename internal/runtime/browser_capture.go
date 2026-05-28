package runtime

import (
	"fmt"

	"github.com/tales-testing/tales/internal/provider"
	browserprovider "github.com/tales-testing/tales/internal/provider/browser"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/function"
)

// browserCaptureScope returns the (functions, variables) pair the runtime
// injects into the EvalContext when evaluating a browser step's capture
// expressions. The snapshot recorded by the provider after the step ran
// is used to back text() / attribute() / browser.url / browser.title so
// the user does not have to re-issue CDP calls from inside HCL.
func browserCaptureScope(providerImpl provider.Provider, scenarioName, stepName string) (map[string]function.Function, map[string]cty.Value) {
	bp, ok := providerImpl.(*browserprovider.Provider)
	if !ok {
		return nil, nil
	}

	snap, _ := bp.LastSnapshot(scenarioName, stepName)

	return map[string]function.Function{
			"text":      browserTextFunction(snap),
			"attribute": browserAttributeFunction(snap),
		},
		map[string]cty.Value{
			"browser": browserNamespaceValue(snap),
		}
}

func browserTextFunction(snap *browserprovider.Snapshot) function.Function {
	return function.New(&function.Spec{
		Params: []function.Parameter{{Name: keySelector, Type: cty.String}},
		Type:   function.StaticReturnType(cty.String),
		Impl: func(args []cty.Value, _ cty.Type) (cty.Value, error) {
			if snap == nil {
				return cty.NilVal, fmt.Errorf("text: no DOM snapshot available")
			}

			node, err := findFirstNode(snap.DOM, args[0].AsString())
			if err != nil {
				return cty.NilVal, err
			}

			return cty.StringVal(nodeText(node)), nil
		},
	})
}

func browserAttributeFunction(snap *browserprovider.Snapshot) function.Function {
	return function.New(&function.Spec{
		Params: []function.Parameter{
			{Name: keySelector, Type: cty.String},
			{Name: keyName, Type: cty.String},
		},
		Type: function.StaticReturnType(cty.String),
		Impl: func(args []cty.Value, _ cty.Type) (cty.Value, error) {
			if snap == nil {
				return cty.NilVal, fmt.Errorf("attribute: no DOM snapshot available")
			}

			node, err := findFirstNode(snap.DOM, args[0].AsString())
			if err != nil {
				return cty.NilVal, err
			}

			value, _ := nodeAttr(node, args[1].AsString())

			return cty.StringVal(value), nil
		},
	})
}

func browserNamespaceValue(snap *browserprovider.Snapshot) cty.Value {
	if snap == nil {
		return cty.ObjectVal(map[string]cty.Value{
			keyURL:   cty.StringVal(""),
			keyTitle: cty.StringVal(""),
		})
	}

	return cty.ObjectVal(map[string]cty.Value{
		keyURL:   cty.StringVal(snap.URL),
		keyTitle: cty.StringVal(snap.Title),
	})
}
