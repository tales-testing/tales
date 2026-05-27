package diagnostic

import (
	"encoding/json"
	"fmt"
	"math/big"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/zclconf/go-cty/cty"
)

const maskedValue = "***"
const (
	boolStringTrue  = "true"
	boolStringFalse = "false"
	jsonKey         = "json"
)

var sensitiveHeaders = map[string]struct{}{
	"authorization":       {},
	"proxy-authorization": {},
	"cookie":              {},
	"set-cookie":          {},
	"x-api-key":           {},
	"x-auth-token":        {},
}

var sensitiveJSONFields = map[string]struct{}{
	"password":      {},
	"passwd":        {},
	"token":         {},
	"access_token":  {},
	"refresh_token": {},
	"secret":        {},
	"api_key":       {},
	"client_secret": {},
}

// FromCTYMap converts cty maps to plain Go values and applies secret masking.
func FromCTYMap(values map[string]cty.Value) map[string]interface{} {
	converted := make(map[string]interface{}, len(values))

	for key, value := range values {
		converted[key] = FromCTY(value)
	}

	return SanitizeMap(converted)
}

// FromCTY converts a cty value into plain Go values suitable for reports.
func FromCTY(value cty.Value) interface{} {
	if !value.IsKnown() || value.IsNull() {
		return nil
	}

	switch {
	case value.Type() == cty.String:
		return value.AsString()
	case value.Type() == cty.Bool:
		return value.True()
	case value.Type() == cty.Number:
		return numberToGo(value)
	case value.Type().IsObjectType() || value.Type().IsMapType():
		mapped := make(map[string]interface{}, len(value.AsValueMap()))
		for key, nested := range value.AsValueMap() {
			mapped[key] = FromCTY(nested)
		}

		return mapped
	case value.Type().IsTupleType() || value.Type().IsListType() || value.Type().IsSetType():
		items := make([]interface{}, 0, value.LengthInt())
		for it := value.ElementIterator(); it.Next(); {
			_, nested := it.Element()
			items = append(items, FromCTY(nested))
		}

		return items
	default:
		return fmt.Sprintf("%v", value)
	}
}

// Normalize converts arbitrary values (including cty.Value) into plain report-safe values.
func Normalize(value interface{}) interface{} {
	if value == nil {
		return nil
	}

	ctyValue, ok := value.(cty.Value)
	if ok {
		return SanitizeUnknown(FromCTY(ctyValue))
	}

	return SanitizeUnknown(value)
}

// SanitizeMap masks secrets in known request/response maps.
func SanitizeMap(values map[string]interface{}) map[string]interface{} {
	if values == nil {
		return nil
	}

	sanitized := make(map[string]interface{}, len(values))

	for key, value := range values {
		switch strings.ToLower(key) {
		case "headers":
			sanitized[key] = MaskHeaders(value)
		case "headers_all":
			sanitized[key] = MaskHeadersAll(value)
		case "cookies":
			sanitized[key] = MaskCookies(value)
		case jsonKey:
			sanitized[key] = MaskJSON(value)
		case "body":
			sanitized[key] = MaskBody(value)
		case "raw_body":
			sanitized[key] = MaskBody(value)
		default:
			sanitized[key] = SanitizeUnknown(value)
		}
	}

	return sanitized
}

// SanitizeUnknown masks secrets recursively by JSON key names.
func SanitizeUnknown(value interface{}) interface{} {
	switch typed := value.(type) {
	case map[string]interface{}:
		masked := make(map[string]interface{}, len(typed))
		for key, nested := range typed {
			if isSensitiveJSONField(key) {
				masked[key] = maskedValue

				continue
			}

			masked[key] = SanitizeUnknown(nested)
		}

		return masked
	case map[string]string:
		masked := make(map[string]interface{}, len(typed))
		for key, nested := range typed {
			if isSensitiveJSONField(key) {
				masked[key] = maskedValue

				continue
			}

			masked[key] = nested
		}

		return masked
	case []interface{}:
		masked := make([]interface{}, 0, len(typed))
		for _, nested := range typed {
			masked = append(masked, SanitizeUnknown(nested))
		}

		return masked
	case []string:
		masked := make([]interface{}, 0, len(typed))
		for _, nested := range typed {
			masked = append(masked, nested)
		}

		return masked
	default:
		return typed
	}
}

// MaskHeaders masks sensitive headers case-insensitively.
func MaskHeaders(value interface{}) map[string]string {
	headers := map[string]string{}

	switch typed := value.(type) {
	case map[string]interface{}:
		for key, nested := range typed {
			headers[key] = stringify(nested)
		}
	case map[string]string:
		for key, nested := range typed {
			headers[key] = nested
		}
	default:
		return headers
	}

	for key, current := range headers {
		if isSensitiveHeader(key) {
			if strings.HasPrefix(strings.ToLower(current), "basic ") {
				headers[key] = "Basic " + maskedValue
			} else {
				headers[key] = maskedValue
			}

			continue
		}

		headers[key] = current
	}

	return headers
}

// MaskHeadersAll masks sensitive headers in the multi-value response shape
// (map[string][]string). Every value of a sensitive header is replaced with
// the placeholder; non-sensitive headers pass through unchanged.
func MaskHeadersAll(value interface{}) map[string][]string {
	result := map[string][]string{}

	switch typed := value.(type) {
	case map[string]interface{}:
		for key, nested := range typed {
			result[key] = headerValuesToStrings(nested)
		}
	case map[string][]string:
		for key, values := range typed {
			cloned := make([]string, len(values))
			copy(cloned, values)
			result[key] = cloned
		}
	default:
		return result
	}

	for key, values := range result {
		if !isSensitiveHeader(key) {
			continue
		}

		masked := make([]string, len(values))
		for i := range values {
			masked[i] = maskedValue
		}

		result[key] = masked
	}

	return result
}

// MaskCookies redacts the value and raw fields of every cookie in the
// response.cookies object. The other fields (name, path, domain, expires,
// max_age, secure, http_only, same_site) pass through unchanged so debugging
// information stays useful.
func MaskCookies(value interface{}) map[string]map[string]interface{} {
	result := map[string]map[string]interface{}{}

	source, ok := value.(map[string]interface{})
	if !ok {
		return result
	}

	for name, raw := range source {
		fields, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}

		masked := make(map[string]interface{}, len(fields))

		for key, val := range fields {
			lowered := strings.ToLower(key)
			if lowered == "value" || lowered == "raw" {
				if str, isStr := val.(string); isStr && str == "" {
					masked[key] = ""

					continue
				}

				masked[key] = maskedValue

				continue
			}

			masked[key] = val
		}

		result[name] = masked
	}

	return result
}

func headerValuesToStrings(value interface{}) []string {
	switch typed := value.(type) {
	case []string:
		cloned := make([]string, len(typed))
		copy(cloned, typed)

		return cloned
	case []interface{}:
		values := make([]string, 0, len(typed))
		for _, item := range typed {
			values = append(values, stringify(item))
		}

		return values
	case string:
		return []string{typed}
	default:
		return []string{stringify(typed)}
	}
}

// MaskJSON masks sensitive fields recursively in maps/arrays.
func MaskJSON(value interface{}) interface{} {
	switch typed := value.(type) {
	case map[string]interface{}:
		masked := make(map[string]interface{}, len(typed))
		for key, nested := range typed {
			if isSensitiveJSONField(key) {
				masked[key] = maskedValue

				continue
			}

			masked[key] = MaskJSON(nested)
		}

		return masked
	case map[string]string:
		masked := make(map[string]interface{}, len(typed))
		for key, nested := range typed {
			if isSensitiveJSONField(key) {
				masked[key] = maskedValue

				continue
			}

			masked[key] = nested
		}

		return masked
	case []interface{}:
		masked := make([]interface{}, 0, len(typed))
		for _, nested := range typed {
			masked = append(masked, MaskJSON(nested))
		}

		return masked
	default:
		return typed
	}
}

// MaskBody masks JSON bodies when possible, otherwise returns the original body.
func MaskBody(value interface{}) interface{} {
	switch typed := value.(type) {
	case map[string]interface{}:
		masked := make(map[string]interface{}, len(typed))
		for key, nested := range typed {
			switch strings.ToLower(key) {
			case jsonKey, "form":
				masked[key] = MaskJSON(nested)
			case "raw":
				masked[key] = MaskBody(nested)
			default:
				masked[key] = SanitizeUnknown(nested)
			}
		}

		return masked
	case map[string]string:
		return MaskJSON(typed)
	}

	body, ok := value.(string)
	if !ok {
		return value
	}

	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		return body
	}

	if !looksLikeJSON(trimmed) {
		return MaskFormBody(body)
	}

	var decoded interface{}
	if err := json.Unmarshal([]byte(trimmed), &decoded); err != nil {
		return body
	}

	masked := MaskJSON(decoded)

	encoded, err := json.Marshal(masked)
	if err != nil {
		return body
	}

	return string(encoded)
}

// MaskFormBody masks sensitive application/x-www-form-urlencoded fields when a body looks like form data.
func MaskFormBody(body string) interface{} {
	if !strings.Contains(body, "=") {
		return body
	}

	values, err := url.ParseQuery(body)
	if err != nil || len(values) == 0 {
		return body
	}

	masked := url.Values{}

	for key, items := range values {
		if isSensitiveJSONField(key) {
			masked.Set(key, maskedValue)

			continue
		}

		for _, item := range items {
			masked.Add(key, item)
		}
	}

	return strings.ReplaceAll(masked.Encode(), url.QueryEscape(maskedValue), maskedValue)
}

// ScalarString renders simple values in a stable human-friendly way.
func ScalarString(value interface{}) string {
	sanitized := Normalize(value)
	if sanitized == nil {
		return "null"
	}

	switch typed := sanitized.(type) {
	case string:
		return typed
	case bool:
		if typed {
			return boolStringTrue
		}

		return boolStringFalse
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		encoded, err := json.Marshal(typed)
		if err != nil {
			return fmt.Sprintf("%v", typed)
		}

		return string(encoded)
	}
}

// PrettyJSON renders maps/slices as indented JSON.
func PrettyJSON(value interface{}) string {
	sanitized := Normalize(value)

	encoded, err := json.MarshalIndent(sanitized, "", "  ")
	if err != nil {
		return ScalarString(sanitized)
	}

	return string(encoded)
}

// SortedHeaderKeys returns deterministic sorted keys.
func SortedHeaderKeys(headers map[string]string) []string {
	keys := make([]string, 0, len(headers))
	for key := range headers {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	return keys
}

func numberToGo(value cty.Value) interface{} {
	bf := value.AsBigFloat()
	if bf == nil {
		return 0
	}

	if bf.IsInt() {
		i := new(big.Int)
		bf.Int(i)

		if i.IsInt64() {
			return i.Int64()
		}

		return i.String()
	}

	f, _ := bf.Float64()

	return f
}

func stringify(value interface{}) string {
	switch typed := value.(type) {
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case bool:
		if typed {
			return boolStringTrue
		}

		return boolStringFalse
	default:
		encoded, err := json.Marshal(typed)
		if err != nil {
			return fmt.Sprintf("%v", typed)
		}

		return string(encoded)
	}
}

func isSensitiveHeader(name string) bool {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if _, ok := sensitiveHeaders[normalized]; ok {
		return true
	}

	// Any header whose name contains "signature" carries signing material.
	// This covers X-Anchorify-Signature, X-Hub-Signature, X-My-Signature-Token,
	// etc. without having to enumerate every vendor-specific name.
	return strings.Contains(normalized, "signature")
}

func isSensitiveJSONField(name string) bool {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if _, ok := sensitiveJSONFields[normalized]; ok {
		return true
	}

	// Catch *_secret-style names (mfa_secret, totp_secret, hmac_secret, ...)
	// without falsely matching unrelated words. We require either a boundary
	// hit ("secret") or a contains-with-underscore-or-dash neighbor so plain
	// "secretary" does not get masked.
	if normalized == "secret" {
		return true
	}

	if strings.HasSuffix(normalized, "_secret") || strings.HasSuffix(normalized, "-secret") {
		return true
	}

	if strings.HasPrefix(normalized, "secret_") || strings.HasPrefix(normalized, "secret-") {
		return true
	}

	return false
}

func looksLikeJSON(value string) bool {
	return strings.HasPrefix(value, "{") || strings.HasPrefix(value, "[")
}
