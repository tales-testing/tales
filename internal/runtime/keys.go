package runtime

// Centralized string constants shared across the runtime package. They keep
// the report error kinds, output object keys and phase labels in one place.
const (
	kindEval     = "eval"
	kindProvider = "provider"
	kindCapture  = "capture"
	kindVars     = "vars"
	kindKeyword  = "keyword"
	kindSkip     = "skip"
	kindSave     = "save"

	outputRequest  = "request"
	outputResponse = "response"

	phaseStep     = "step"
	phaseTeardown = "teardown"

	// whenExprPathTeardown / whenExprPathStep prefix the generator seed
	// mixer for a `when` expression. Scenario teardown keeps its historical
	// prefix so already-recorded suites keep replaying identical generated
	// values; every other phase uses the step prefix.
	whenExprPathTeardown = "teardown.when"
	whenExprPathStep     = "step.when"

	// whenFalseReason is the skip reason recorded when a `when` predicate
	// evaluates to false. The wording matches the public documentation.
	whenFalseReason = "when condition was false"

	attrKind    = "kind"
	keyName     = "name"
	keyPassword = "password"
	keyValue    = "value"
	keySelector = "selector"
	keyTarget   = "target"
	keyText     = "text"
	keyTitle    = "title"
	keyURL      = "url"
	keyMasked   = "***"
	keyPath     = "path"
	keyExists   = "exists"
	keyJSON     = "json"
	keySize     = "size_bytes"

	keyPerformance          = "performance"
	keyDOMContentLoadedMS   = "dom_content_loaded_ms"
	keyLoadEventMS          = "load_event_ms"
	keyFCPMS                = "fcp_ms"
	keyLCPMS                = "lcp_ms"
	keyCLS                  = "cls"
	keyResourcesCount       = "resources_count"
	keyTransferSizeBytes    = "transfer_size_bytes"
	keyEncodedBodySizeBytes = "encoded_body_size_bytes"
	keyDecodedBodySizeBytes = "decoded_body_size_bytes"

	kindRuntime = "runtime"
)
