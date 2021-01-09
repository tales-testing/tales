package lang

import (
	"sync"

	"github.com/zclconf/go-cty/cty/function"
)

// Scope is the main type in this package, allowing dynamic evaluation of
// blocks and expressions based on some contextual information that informs
// which variables and functions will be available.
type Scope struct {
	funcs     map[string]function.Function
	funcsLock sync.Mutex
}
