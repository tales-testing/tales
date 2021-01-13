package configs

import (
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"strings"
	"time"

	"github.com/hyperxlab/tales/pkg/tales/reporter"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/gohcl"
	"github.com/zclconf/go-cty/cty"
	ctyjson "github.com/zclconf/go-cty/cty/json"
)

// HTTPRequest struct
type HTTPRequest struct {
	URL     string            `hcl:"url"`
	Method  string            `hcl:"method"`
	Headers map[string]string `hcl:"headers,optional"`
	Body    string            `hcl:"body,optional"`
}

// HTTPResponse struct
type HTTPResponse struct {
	StatusCode int               `hcl:"status_code,optional"`
	Headers    map[string]string `hcl:"headers,optional"`
	Body       string            `hcl:"body,optional"`
}

// HTTPCase struct
type HTTPCase struct {
	Name     string
	Request  HTTPRequest  `hcl:"request,block"`
	Response HTTPResponse `hcl:"response,block"`
	result   *reporter.Case
}

// Parse implements TestCase
func (r *HTTPCase) Parse(c *Case, ctx *hcl.EvalContext) hcl.Diagnostics {
	r.Name = c.Name

	return gohcl.DecodeBody(c.HCL, ctx, r)
}

// Result implements TestCase
func (r *HTTPCase) Result() *reporter.Case {
	if r.result.Name == "" {
		r.result.Name = r.Name
		r.result.Status = reporter.StatusNotExecuted
	}

	return r.result
}

// Execute implements TestCase
func (r *HTTPCase) Execute(ctx *hcl.EvalContext) (result *reporter.Case) {
	result = &reporter.Case{
		Name: r.Name,
	}

	now := time.Now()

	defer func() {
		result.Duration = time.Since(now)

		r.result = result
	}()

	var body io.Reader

	if r.Request.Body != "" {
		body = strings.NewReader(r.Request.Body)
	}

	req, err := http.NewRequest(r.Request.Method, r.Request.URL, body)
	if err != nil {
		result.FromError(err)

		return
	}

	for key, val := range r.Request.Headers {
		req.Header.Set(key, val)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		result.FromError(err)

		return
	}

	if r.Response.StatusCode != 0 {
		if resp.StatusCode != r.Response.StatusCode {
			result.FromError(fmt.Errorf("status code %d is not equal to %d", resp.StatusCode, r.Response.StatusCode))

			return
		}
	}

	if len(r.Response.Headers) > 0 {
		for key, val := range r.Response.Headers {
			v := resp.Header.Get(key)
			if v != val {
				result.FromError(fmt.Errorf("response header %s is not equal to %s", key, val))

				return
			}
		}
	}

	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		result.FromError(fmt.Errorf("read response body failed: %w", err))

		return
	}
	defer resp.Body.Close()

	inputType, err := ctyjson.ImpliedType(respBody)
	if err != nil {
		result.FromError(fmt.Errorf("detect type of response body failed: %w", err))

		return
	}

	bodyValue, err := ctyjson.Unmarshal(respBody, inputType)
	if err != nil {
		result.FromError(fmt.Errorf("convert response body failed: %w", err))

		return
	}

	result.Status = reporter.StatusPassed

	httpVars := ctx.Variables["http"].AsValueMap()
	if httpVars == nil {
		httpVars = map[string]cty.Value{}
	}

	httpVars[r.Name] = cty.ObjectVal(map[string]cty.Value{
		"status_code": cty.NumberIntVal(int64(r.Response.StatusCode)),
		"body":        bodyValue,
	})

	ctx.Variables["http"] = cty.ObjectVal(httpVars)

	return
}
