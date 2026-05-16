package lang

import (
	goruntime "runtime"

	"github.com/zclconf/go-cty/cty"
)

// hostObject returns the cty value exposed to expressions as the
// `host` variable. It surfaces the Go runtime OS and architecture so
// that scenarios can gate themselves on platform without provider
// support.
//
// The object is intentionally minimal in V1 (os, arch) and may grow
// later (ci, hostname, ...). New fields must remain side-effect-free
// and cheap to read.
func hostObject() cty.Value {
	return cty.ObjectVal(map[string]cty.Value{
		"os":   cty.StringVal(goruntime.GOOS),
		"arch": cty.StringVal(goruntime.GOARCH),
	})
}
