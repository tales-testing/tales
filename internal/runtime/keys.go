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

	outputRequest  = "request"
	outputResponse = "response"

	phaseStep = "step"

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
