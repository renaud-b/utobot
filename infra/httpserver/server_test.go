package httpserver

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dop251/goja"
	"utobot/infra/runtime"
)

type fakeRuntimeManager struct {
	route     runtime.Route
	params    map[string]string
	hasRoute  bool
	result    goja.Value
	invokedAs runtime.Route
	payload   any
	invokeErr error
}

func (f *fakeRuntimeManager) LoadSource(path string, source []byte) error { return nil }

func (f *fakeRuntimeManager) Routes() map[string]runtime.Route { return nil }

func (f *fakeRuntimeManager) Match(method, path string) (runtime.Route, map[string]string, bool) {
	if f.hasRoute {
		return f.route, f.params, true
	}
	return runtime.Route{}, nil, false
}

func (f *fakeRuntimeManager) InvokeHandler(route runtime.Route, data any) (goja.Value, error) {
	f.invokedAs = route
	f.payload = data
	return f.result, f.invokeErr
}

func (f *fakeRuntimeManager) Path() string { return "" }

func TestServerRoutesRequestToRuntimeHandler(t *testing.T) {
	rt := goja.New()
	manager := &fakeRuntimeManager{
		hasRoute: true,
		route: runtime.Route{
			Method: http.MethodPost,
			Path:   "/users/:id",
		},
		params: map[string]string{"id": "42"},
		result: rt.ToValue(map[string]any{
			"status":      202,
			"contentType": "application/vnd.example+json",
			"headers": map[string]any{
				"x-test": "yes",
			},
			"body": map[string]any{
				"message": "Hello, Codex!",
			},
		}),
	}

	server := New(8081, manager)
	req := httptest.NewRequest(http.MethodPost, "http://example.com/users/42", strings.NewReader(`{"name":"Codex"}`))
	req.Header.Set("Origin", "https://example.com")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-Id", "abc-123")
	rr := httptest.NewRecorder()

	server.ServeHTTP(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("unexpected status: got %d want %d", rr.Code, http.StatusAccepted)
	}

	if got := rr.Header().Get("x-test"); got != "yes" {
		t.Fatalf("unexpected response header: %q", got)
	}

	if got := rr.Header().Get("content-type"); got != "application/vnd.example+json" {
		t.Fatalf("unexpected content-type: %q", got)
	}
	if got := rr.Header().Get("access-control-allow-origin"); got != "*" {
		t.Fatalf("unexpected CORS header: %q", got)
	}

	if manager.invokedAs.Method != http.MethodPost {
		t.Fatalf("unexpected route method invoked: %q", manager.invokedAs.Method)
	}
	if manager.invokedAs.Path != "/users/:id" {
		t.Fatalf("unexpected route path invoked: %q", manager.invokedAs.Path)
	}

	payload, ok := manager.payload.(map[string]any)
	if !ok {
		t.Fatalf("unexpected payload type: %T", manager.payload)
	}

	if got := payload["method"]; got != http.MethodPost {
		t.Fatalf("unexpected method payload: %#v", got)
	}
	if got := payload["path"]; got != "/users/42" {
		t.Fatalf("unexpected path payload: %#v", got)
	}
	params, ok := payload["params"].(map[string]string)
	if !ok {
		t.Fatalf("unexpected params payload type: %T", payload["params"])
	}
	if got := params["id"]; got != "42" {
		t.Fatalf("unexpected params payload value: %#v", got)
	}
	if got := payload["requestId"]; got != "abc-123" {
		t.Fatalf("unexpected request id payload: %#v", got)
	}

	if got := payload["form"]; got != nil {
		t.Fatalf("expected nil form payload for JSON request, got: %#v", got)
	}

	jsonBody, ok := payload["json"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected json payload type: %T", payload["json"])
	}
	if got := jsonBody["name"]; got != "Codex" {
		t.Fatalf("unexpected json payload value: %#v", got)
	}

	response := map[string]any{}
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
	body, ok := response["message"].(string)
	if !ok {
		t.Fatalf("unexpected response body type: %T", response["message"])
	}
	if body != "Hello, Codex!" {
		t.Fatalf("unexpected response message: %#v", body)
	}
}

func TestServerParsesFormUrlencodedPayload(t *testing.T) {
	rt := goja.New()
	manager := &fakeRuntimeManager{
		hasRoute: true,
		route: runtime.Route{
			Method: http.MethodPost,
			Path:   "/contact",
		},
		result: rt.ToValue(map[string]any{
			"status": 200,
			"body":   "ok",
		}),
	}

	server := New(8081, manager)
	req := httptest.NewRequest(http.MethodPost, "http://example.com/contact", strings.NewReader("name=Codex&email=codex%40example.com&tag=a&tag=b"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded;charset=UTF-8")
	rr := httptest.NewRecorder()

	server.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: got %d want %d", rr.Code, http.StatusOK)
	}

	payload, ok := manager.payload.(map[string]any)
	if !ok {
		t.Fatalf("unexpected payload type: %T", manager.payload)
	}

	form, ok := payload["form"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected form payload type: %T", payload["form"])
	}
	if got := form["name"]; got != "Codex" {
		t.Fatalf("unexpected form name: %#v", got)
	}
	if got := form["email"]; got != "codex@example.com" {
		t.Fatalf("unexpected form email: %#v", got)
	}
	tags, ok := form["tag"].([]string)
	if !ok {
		t.Fatalf("unexpected form tag type: %T", form["tag"])
	}
	if len(tags) != 2 || tags[0] != "a" || tags[1] != "b" {
		t.Fatalf("unexpected form tags: %#v", tags)
	}
}

func TestServerHandlesCORSPreflight(t *testing.T) {
	manager := &fakeRuntimeManager{
		hasRoute: true,
		route: runtime.Route{
			Method: http.MethodPost,
			Path:   "/submit",
		},
	}
	server := New(8081, manager)

	req := httptest.NewRequest(http.MethodOptions, "http://example.com/submit", nil)
	req.Header.Set("Origin", "https://example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "content-type, x-request-id")
	rr := httptest.NewRecorder()

	server.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("unexpected status: got %d want %d", rr.Code, http.StatusNoContent)
	}
	if got := rr.Header().Get("access-control-allow-origin"); got != "*" {
		t.Fatalf("unexpected allow-origin: %q", got)
	}
	if got := rr.Header().Get("access-control-allow-methods"); got != "POST" {
		t.Fatalf("unexpected allow-methods: %q", got)
	}
	if got := rr.Header().Get("access-control-allow-headers"); got != "content-type, x-request-id" {
		t.Fatalf("unexpected allow-headers: %q", got)
	}
	if got := rr.Header().Get("access-control-max-age"); got != "86400" {
		t.Fatalf("unexpected max-age: %q", got)
	}
}

func TestServerReturns404ForMissingRoute(t *testing.T) {
	manager := &fakeRuntimeManager{}
	server := New(8081, manager)

	req := httptest.NewRequest(http.MethodGet, "http://example.com/missing", nil)
	rr := httptest.NewRecorder()

	server.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("unexpected status: got %d want %d", rr.Code, http.StatusNotFound)
	}
}

func TestServerReturnsDetailedErrorPageForRuntimeError(t *testing.T) {
	manager := &fakeRuntimeManager{
		hasRoute: true,
		route: runtime.Route{
			Method: http.MethodGet,
			Path:   "/boom",
		},
		invokeErr: errors.New("invoke handler GET /boom: Error: exploded\n    at boom (/runtime.js:3:9)"),
	}
	server := New(8081, manager)

	req := httptest.NewRequest(http.MethodGet, "http://example.com/boom", nil)
	req.Header.Set("X-Request-Id", "req-42")
	rr := httptest.NewRecorder()

	server.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("unexpected status: got %d want %d", rr.Code, http.StatusInternalServerError)
	}
	if got := rr.Header().Get("content-type"); got != "text/html; charset=utf-8" {
		t.Fatalf("unexpected content-type: %q", got)
	}

	body := rr.Body.String()
	for _, want := range []string{
		"Runtime script error",
		"GET /boom",
		"req-42",
		"Error: exploded",
		"at boom (/runtime.js:3:9)",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected error page to contain %q, got:\n%s", want, body)
		}
	}
}

func TestServerEscapesDetailedErrorPage(t *testing.T) {
	manager := &fakeRuntimeManager{
		hasRoute: true,
		route: runtime.Route{
			Method: http.MethodGet,
			Path:   "/boom",
		},
		invokeErr: errors.New(`<script>alert("xss")</script>`),
	}
	server := New(8081, manager)

	req := httptest.NewRequest(http.MethodGet, "http://example.com/boom", nil)
	rr := httptest.NewRecorder()

	server.ServeHTTP(rr, req)

	body := rr.Body.String()
	if strings.Contains(body, "<script>") {
		t.Fatalf("expected script tag to be escaped, got:\n%s", body)
	}
	if !strings.Contains(body, "&lt;script&gt;") {
		t.Fatalf("expected escaped script tag, got:\n%s", body)
	}
}
