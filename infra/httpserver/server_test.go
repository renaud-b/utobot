package httpserver

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dop251/goja"
	"utobot/infra/brain"
)

type fakeBrainManager struct {
	route     brain.Route
	params    map[string]string
	hasRoute  bool
	result    goja.Value
	invokedAs brain.Route
	payload   any
	invokeErr error
}

func (f *fakeBrainManager) LoadSource(path string, source []byte) error { return nil }

func (f *fakeBrainManager) Routes() map[string]brain.Route { return nil }

func (f *fakeBrainManager) Match(method, path string) (brain.Route, map[string]string, bool) {
	if f.hasRoute {
		return f.route, f.params, true
	}
	return brain.Route{}, nil, false
}

func (f *fakeBrainManager) InvokeHandler(route brain.Route, data any) (goja.Value, error) {
	f.invokedAs = route
	f.payload = data
	return f.result, f.invokeErr
}

func (f *fakeBrainManager) Path() string { return "" }

func TestServerRoutesRequestToBrainHandler(t *testing.T) {
	rt := goja.New()
	manager := &fakeBrainManager{
		hasRoute: true,
		route: brain.Route{
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

func TestServerReturns404ForMissingRoute(t *testing.T) {
	manager := &fakeBrainManager{}
	server := New(8081, manager)

	req := httptest.NewRequest(http.MethodGet, "http://example.com/missing", nil)
	rr := httptest.NewRecorder()

	server.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("unexpected status: got %d want %d", rr.Code, http.StatusNotFound)
	}
}

func TestServerReturnsDetailedErrorPageForBrainError(t *testing.T) {
	manager := &fakeBrainManager{
		hasRoute: true,
		route: brain.Route{
			Method: http.MethodGet,
			Path:   "/boom",
		},
		invokeErr: errors.New("invoke handler GET /boom: Error: exploded\n    at boom (/brain.js:3:9)"),
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
		"Brain script error",
		"GET /boom",
		"req-42",
		"Error: exploded",
		"at boom (/brain.js:3:9)",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected error page to contain %q, got:\n%s", want, body)
		}
	}
}

func TestServerEscapesDetailedErrorPage(t *testing.T) {
	manager := &fakeBrainManager{
		hasRoute: true,
		route: brain.Route{
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
