package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/dop251/goja"
)

const defaultFetchTimeout = 30 * time.Second

func bindFetch(runtime *goja.Runtime) error {
	client := &http.Client{Timeout: defaultFetchTimeout}

	return runtime.Set("fetch", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			panic(runtime.NewTypeError("fetch requires a URL"))
		}

		url := strings.TrimSpace(call.Argument(0).String())
		if url == "" {
			panic(runtime.NewTypeError("fetch URL is required"))
		}

		method := http.MethodGet
		methodProvided := false
		headers := make(http.Header)
		var bodyReader io.Reader

		if len(call.Arguments) > 1 {
			initValue := call.Argument(1)
			if !goja.IsUndefined(initValue) && !goja.IsNull(initValue) {
				initObject := initValue.ToObject(runtime)
				if initObject != nil {
					rawMethod := strings.TrimSpace(initObject.Get("method").String())
					if rawMethod != "" {
						method = strings.ToUpper(rawMethod)
						methodProvided = true
					}

					if headersValue := initObject.Get("headers"); headersValue != nil && !goja.IsUndefined(headersValue) && !goja.IsNull(headersValue) {
						copyHeaderMap(headers, headersValue.Export())
					}

					if bodyValue := initObject.Get("body"); bodyValue != nil && !goja.IsUndefined(bodyValue) && !goja.IsNull(bodyValue) {
						encodedBody, inferredContentType, err := encodeFetchBody(bodyValue.Export(), headers)
						if err != nil {
							panic(runtime.NewGoError(err))
						}

						bodyReader = bytes.NewReader(encodedBody)
						if inferredContentType != "" && headers.Get("Content-Type") == "" {
							headers.Set("Content-Type", inferredContentType)
						}
					}
				}
			}
		}

		if !methodProvided && bodyReader != nil && method == http.MethodGet {
			method = http.MethodPost
		}

		req, err := http.NewRequestWithContext(context.Background(), method, url, bodyReader)
		if err != nil {
			panic(runtime.NewGoError(fmt.Errorf("create fetch request: %w", err)))
		}

		req.Header = headers

		resp, err := client.Do(req)
		if err != nil {
			panic(runtime.NewGoError(fmt.Errorf("fetch %s: %w", url, err)))
		}
		defer resp.Body.Close()

		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			panic(runtime.NewGoError(fmt.Errorf("read fetch response: %w", err)))
		}

		return buildFetchResponse(runtime, resp, respBody)
	})
}

func encodeFetchBody(value any, headers http.Header) ([]byte, string, error) {
	switch typed := value.(type) {
	case nil:
		return nil, "", nil
	case string:
		return []byte(typed), "text/plain; charset=utf-8", nil
	case []byte:
		return typed, "", nil
	}

	if headers.Get("Content-Type") == "" {
		headers.Set("Content-Type", "application/json")
	}

	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, "", fmt.Errorf("encode fetch body: %w", err)
	}

	return encoded, "application/json", nil
}

func copyHeaderMap(headers http.Header, value any) {
	if value == nil {
		return
	}

	switch typed := value.(type) {
	case map[string]any:
		for key, rawValue := range typed {
			appendHeaderValue(headers, key, rawValue)
		}
	case map[string]string:
		for key, rawValue := range typed {
			if rawValue != "" {
				headers.Set(key, rawValue)
			}
		}
	}
}

func appendHeaderValue(headers http.Header, key string, value any) {
	switch typed := value.(type) {
	case string:
		if typed != "" {
			headers.Set(key, typed)
		}
	case []string:
		if len(typed) > 0 {
			headers.Set(key, typed[0])
		}
	case []any:
		if len(typed) > 0 {
			headers.Set(key, fmt.Sprint(typed[0]))
		}
	default:
		headers.Set(key, fmt.Sprint(value))
	}
}

func buildFetchResponse(runtime *goja.Runtime, resp *http.Response, body []byte) goja.Value {
	response := runtime.NewObject()

	_ = response.Set("status", resp.StatusCode)
	_ = response.Set("ok", resp.StatusCode >= 200 && resp.StatusCode < 300)
	_ = response.Set("url", resp.Request.URL.String())
	_ = response.Set("statusText", resp.Status)
	_ = response.Set("headers", responseHeaderMap(resp.Header))
	_ = response.Set("body", string(body))

	_ = response.Set("text", func(goja.FunctionCall) goja.Value {
		return runtime.ToValue(string(body))
	})

	_ = response.Set("json", func(goja.FunctionCall) goja.Value {
		var parsed any
		if err := json.Unmarshal(body, &parsed); err != nil {
			panic(runtime.NewGoError(fmt.Errorf("parse fetch response JSON: %w", err)))
		}

		return runtime.ToValue(parsed)
	})

	return response
}

func responseHeaderMap(headers http.Header) map[string]string {
	out := make(map[string]string, len(headers))
	for key, values := range headers {
		if len(values) > 0 {
			out[strings.ToLower(key)] = values[0]
		}
	}
	return out
}
