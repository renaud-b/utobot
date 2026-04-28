package brain

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	walletcrypto "utobot/infra/crypto"
	"utobot/infra/state"
)

type fakeLLMConnector struct {
	prompt string
	model  string
	text   string
	err    error
}

func (f *fakeLLMConnector) GenerateText(ctx context.Context, prompt string) (string, error) {
	f.prompt = prompt
	if f.err != nil {
		return "", f.err
	}
	return f.text, nil
}

func (f *fakeLLMConnector) SetModel(model string) {
	f.model = model
}

func TestManagerLoadSourceAndInvokeHandler(t *testing.T) {
	manager := NewManager()

	err := manager.LoadSource("/brain.js", []byte(`
console.log('brain booting');
console.warn('boot warning');
console.debug('boot debug');
console.error('boot error');

module.exports = {
	routes: {
		'POST /users/:id': function sayHello(data) {
			console.log('handling request', data.name);
			console.warn('request warning', data.name);
			console.debug('request debug', data.name);
			console.error('request error', data.name);
			return {
				status: 200,
				body: {
					message: `+"`Hello, ${data.name || 'world'}!`"+`
				}
			};
		},
	},
};
`))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	route, params, ok := manager.Match("POST", "/users/42")
	if !ok {
		t.Fatal("expected route to be registered")
	}
	if route.Method != "POST" {
		t.Fatalf("unexpected route method: %q", route.Method)
	}
	if route.Path != "/users/:id" {
		t.Fatalf("unexpected route path: %q", route.Path)
	}
	if got := params["id"]; got != "42" {
		t.Fatalf("unexpected route param: %q", got)
	}

	result, err := manager.InvokeHandler(route, map[string]any{"name": "Codex"})
	if err != nil {
		t.Fatalf("expected no error invoking handler, got %v", err)
	}

	exported, ok := result.Export().(map[string]any)
	if !ok {
		t.Fatalf("unexpected handler result type: %T", result.Export())
	}

	if got := exported["status"]; got != int64(200) {
		t.Fatalf("unexpected status: %#v", got)
	}

	body, ok := exported["body"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected body type: %T", exported["body"])
	}
	if got := body["message"]; got != "Hello, Codex!" {
		t.Fatalf("unexpected message: %#v", got)
	}
}

func TestManagerAppContextPersistsAcrossInvocations(t *testing.T) {
	manager := NewManager()

	err := manager.LoadSource("/brain.js", []byte(`
module.exports = {
	routes: {
		'POST /state': function updateState(data) {
			state.update(function (ctx) {
				const counter = (ctx.counter || 0) + 1;
				const tags = ctx.tags || [];
				return {
					counter: counter,
					tags: tags.concat([data.tag]),
				};
			});

			return {
				status: 200,
				body: {
					counter: state.get().counter,
					tags: state.get().tags,
				}
			};
		},
	},
};
`))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	route, _, ok := manager.Match("POST", "/state")
	if !ok {
		t.Fatal("expected route to be registered")
	}

	first, err := manager.InvokeHandler(route, map[string]any{"tag": "one"})
	if err != nil {
		t.Fatalf("expected no error invoking handler, got %v", err)
	}

	firstBody, ok := first.Export().(map[string]any)["body"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected first body type: %T", first.Export().(map[string]any)["body"])
	}
	if got := firstBody["counter"]; got != int64(1) {
		t.Fatalf("unexpected first counter: %#v", got)
	}

	second, err := manager.InvokeHandler(route, map[string]any{"tag": "two"})
	if err != nil {
		t.Fatalf("expected no error invoking handler, got %v", err)
	}

	secondBody, ok := second.Export().(map[string]any)["body"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected second body type: %T", second.Export().(map[string]any)["body"])
	}
	if got := secondBody["counter"]; got != int64(2) {
		t.Fatalf("unexpected second counter: %#v", got)
	}

	tags, ok := secondBody["tags"].([]any)
	if !ok {
		t.Fatalf("unexpected tags type: %T", secondBody["tags"])
	}
	if len(tags) != 2 || tags[0] != "one" || tags[1] != "two" {
		t.Fatalf("unexpected tags: %#v", tags)
	}
}

func TestManagerAppContextPersistsAcrossReloadsWithSQLite(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "state.db")

	store1, err := state.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("create state store: %v", err)
	}

	key, err := walletcrypto.GenerateEncryptionKey()
	if err != nil {
		t.Fatalf("GenerateEncryptionKey() error = %v", err)
	}

	manager1 := NewManagerWithStateStore(walletcrypto.KeyPair{}, key, nil, store1)
	err = manager1.LoadSource("/brain-v1.js", []byte(`
module.exports = {
	routes: {
		'POST /state': function updateState(data) {
			state.update(function (ctx) {
				ctx.counter = (ctx.counter || 0) + 1;
				return ctx;
			});

			return {
				status: 200,
				body: {
					counter: state.get().counter,
				}
			};
		},
	},
};
`))
	if err != nil {
		t.Fatalf("load first brain: %v", err)
	}

	firstRoute, _, ok := manager1.Match("POST", "/state")
	if !ok {
		t.Fatal("expected first route to be registered")
	}

	first, err := manager1.InvokeHandler(firstRoute, nil)
	if err != nil {
		t.Fatalf("invoke first route: %v", err)
	}
	firstBody, ok := first.Export().(map[string]any)["body"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected first body type: %T", first.Export().(map[string]any)["body"])
	}
	if got := firstBody["counter"]; got != int64(1) {
		t.Fatalf("unexpected first counter: %#v", got)
	}

	second, err := manager1.InvokeHandler(firstRoute, nil)
	if err != nil {
		t.Fatalf("invoke first route again: %v", err)
	}
	secondBody, ok := second.Export().(map[string]any)["body"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected second body type: %T", second.Export().(map[string]any)["body"])
	}
	if got := secondBody["counter"]; got != int64(2) {
		t.Fatalf("unexpected second counter: %#v", got)
	}

	if err := store1.Close(); err != nil {
		t.Fatalf("close first state store: %v", err)
	}

	store2, err := state.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("reopen state store: %v", err)
	}
	defer store2.Close()

	manager2 := NewManagerWithStateStore(walletcrypto.KeyPair{}, key, nil, store2)
	err = manager2.LoadSource("/brain-v2.js", []byte(`
module.exports = {
	routes: {
		'GET /state': function getState() {
			return {
				status: 200,
				body: {
					counter: state.get().counter,
				}
			};
		},
	},
};
`))
	if err != nil {
		t.Fatalf("load second brain: %v", err)
	}

	route, _, ok := manager2.Match("GET", "/state")
	if !ok {
		t.Fatal("expected second route to be registered")
	}

	result, err := manager2.InvokeHandler(route, nil)
	if err != nil {
		t.Fatalf("invoke second route: %v", err)
	}

	body, ok := result.Export().(map[string]any)["body"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected body type: %T", result.Export().(map[string]any)["body"])
	}
	if got := body["counter"]; got != int64(2) {
		t.Fatalf("unexpected persisted counter: %#v", got)
	}
}

func TestManagerAppStateSupportsParallelUpdates(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "state.db")

	store, err := state.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("create state store: %v", err)
	}
	defer store.Close()

	key, err := walletcrypto.GenerateEncryptionKey()
	if err != nil {
		t.Fatalf("GenerateEncryptionKey() error = %v", err)
	}

	manager := NewManagerWithStateStore(walletcrypto.KeyPair{}, key, nil, store)
	err = manager.LoadSource("/brain.js", []byte(`
module.exports = {
	routes: {
		'POST /inc': function inc() {
			state.update(function (ctx) {
				ctx.counter = (ctx.counter || 0) + 1;
				return ctx;
			});

			return { status: 200, body: { counter: state.get().counter } };
		},
		'GET /count': function count() {
			return { status: 200, body: { counter: state.get().counter || 0 } };
		},
	},
};
`))
	if err != nil {
		t.Fatalf("load brain: %v", err)
	}

	incRoute, _, ok := manager.Match("POST", "/inc")
	if !ok {
		t.Fatal("expected increment route to be registered")
	}
	countRoute, _, ok := manager.Match("GET", "/count")
	if !ok {
		t.Fatal("expected count route to be registered")
	}

	const workers = 20
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := manager.InvokeHandler(incRoute, nil); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("parallel increment failed: %v", err)
	}

	result, err := manager.InvokeHandler(countRoute, nil)
	if err != nil {
		t.Fatalf("invoke count route: %v", err)
	}

	body, ok := result.Export().(map[string]any)["body"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected body type: %T", result.Export().(map[string]any)["body"])
	}
	if got := body["counter"]; got != int64(workers) {
		t.Fatalf("unexpected parallel counter: %#v", got)
	}
}

func TestManagerFetchConnector(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/text":
			w.Header().Set("X-Reply", "pong")
			_, _ = io.WriteString(w, "hello world")
		case "/json":
			rawBody, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read request body: %v", err)
			}
			_ = r.Body.Close()

			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Reply", "pong")
			if err := json.NewEncoder(w).Encode(map[string]any{
				"method":      r.Method,
				"contentType": r.Header.Get("Content-Type"),
				"xTest":       r.Header.Get("X-Test"),
				"body":        string(rawBody),
			}); err != nil {
				t.Fatalf("encode response: %v", err)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	manager := NewManager()

	err := manager.LoadSource("/brain.js", []byte(fmt.Sprintf(`
module.exports = {
	routes: {
		'GET /fetch': function fetchRoute() {
			const textResponse = fetch(%q + '/text');
			const jsonResponse = fetch(%q + '/json', {
				method: 'POST',
				headers: { 'X-Test': 'yes' },
				body: { message: 'hello' },
			});

			const parsed = jsonResponse.json();

			return {
				status: 200,
				body: {
					textStatus: textResponse.status,
					textOk: textResponse.ok,
					textHeader: textResponse.headers['x-reply'],
					textValue: textResponse.text(),
					jsonStatus: jsonResponse.status,
					jsonOk: jsonResponse.ok,
					jsonHeader: jsonResponse.headers['x-reply'],
					jsonMethod: parsed.method,
					jsonContentType: parsed.contentType,
					jsonXTest: parsed.xTest,
					jsonBody: parsed.body,
				},
			};
		},
	},
};
`, server.URL, server.URL)))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	route, _, ok := manager.Match("GET", "/fetch")
	if !ok {
		t.Fatal("expected route to be registered")
	}

	result, err := manager.InvokeHandler(route, nil)
	if err != nil {
		t.Fatalf("expected no error invoking handler, got %v", err)
	}

	body, ok := result.Export().(map[string]any)["body"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected body type: %T", result.Export().(map[string]any)["body"])
	}
	if body["textStatus"] != int64(200) || body["textOk"] != true {
		t.Fatalf("unexpected text response status: %#v %#v", body["textStatus"], body["textOk"])
	}
	if body["textHeader"] != "pong" {
		t.Fatalf("unexpected text header: %#v", body["textHeader"])
	}
	if body["textValue"] != "hello world" {
		t.Fatalf("unexpected text value: %#v", body["textValue"])
	}
	if body["jsonStatus"] != int64(200) || body["jsonOk"] != true {
		t.Fatalf("unexpected json response status: %#v %#v", body["jsonStatus"], body["jsonOk"])
	}
	if body["jsonHeader"] != "pong" {
		t.Fatalf("unexpected json header: %#v", body["jsonHeader"])
	}
	if body["jsonMethod"] != "POST" {
		t.Fatalf("unexpected json method: %#v", body["jsonMethod"])
	}
	if body["jsonContentType"] != "application/json" {
		t.Fatalf("unexpected json content type: %#v", body["jsonContentType"])
	}
	if body["jsonXTest"] != "yes" {
		t.Fatalf("unexpected json X-Test header: %#v", body["jsonXTest"])
	}
	if body["jsonBody"] != "{\"message\":\"hello\"}" {
		t.Fatalf("unexpected json body: %#v", body["jsonBody"])
	}
}

func TestManagerAutonomousLoopRunsConfiguredFunction(t *testing.T) {
	manager := NewManager()

	err := manager.LoadSource("/brain.js", []byte(`
app.loop(5, function autonomousTick() {
	state.update(function (ctx) {
		ctx.counter = (ctx.counter || 0) + 1;
		return ctx;
	});
});

module.exports = {
	routes: {
		'GET /counter': function counterRoute() {
			return {
				status: 200,
				body: { counter: state.get().counter },
			};
		},
	},
};
`))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	route, _, ok := manager.Match("GET", "/counter")
	if !ok {
		t.Fatal("expected route to be registered")
	}

	deadline := time.Now().Add(250 * time.Millisecond)
	for time.Now().Before(deadline) {
		result, err := manager.InvokeHandler(route, nil)
		if err != nil {
			t.Fatalf("expected no error invoking handler, got %v", err)
		}

		body, ok := result.Export().(map[string]any)["body"].(map[string]any)
		if !ok {
			t.Fatalf("unexpected body type: %T", result.Export().(map[string]any)["body"])
		}

		if counter, ok := body["counter"].(int64); ok && counter > 0 {
			return
		}

		time.Sleep(10 * time.Millisecond)
	}

	t.Fatal("expected autonomous loop to increment counter")
}

func TestManagerAutonomousLoopIsReplacedOnReload(t *testing.T) {
	manager := NewManager()

	err := manager.LoadSource("/brain-v1.js", []byte(`
app.loop(5, function autonomousTick() {
	state.update(function (ctx) {
		ctx.counter = (ctx.counter || 0) + 1;
		return ctx;
	});
});

module.exports = {
	routes: {
		'GET /counter': function counterRoute() {
			return { status: 200, body: { version: 1, counter: state.get().counter } };
		},
	},
};
`))
	if err != nil {
		t.Fatalf("expected no error loading first brain, got %v", err)
	}

	firstRoute, _, ok := manager.Match("GET", "/counter")
	if !ok {
		t.Fatal("expected first route to be registered")
	}
	waitForCounter(t, manager, firstRoute, 1)

	err = manager.LoadSource("/brain-v2.js", []byte(`
module.exports = {
	routes: {
		'GET /counter': function counterRoute() {
			return { status: 200, body: { version: 2, counter: state.get().counter } };
		},
	},
};
`))
	if err != nil {
		t.Fatalf("expected no error loading second brain, got %v", err)
	}

	secondRoute, _, ok := manager.Match("GET", "/counter")
	if !ok {
		t.Fatal("expected second route to be registered")
	}

	time.Sleep(30 * time.Millisecond)
	result, err := manager.InvokeHandler(secondRoute, nil)
	if err != nil {
		t.Fatalf("expected no error invoking handler, got %v", err)
	}

	body, ok := result.Export().(map[string]any)["body"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected body type: %T", result.Export().(map[string]any)["body"])
	}
	if got := body["version"]; got != int64(2) {
		t.Fatalf("unexpected brain version: %#v", got)
	}
	if got := body["counter"]; got != int64(2) {
		t.Fatalf("expected second brain without loop to preserve counter at 2, got %#v", got)
	}
}

func TestManagerAutonomousLoopRejectsInvalidConfiguration(t *testing.T) {
	manager := NewManager()

	err := manager.LoadSource("/brain.js", []byte(`
app.loop(0, function () {});

module.exports = {
	routes: {
		'GET /ok': function ok() { return 'ok'; },
	},
};
`))
	if err == nil {
		t.Fatal("expected invalid loop configuration to fail loading")
	}
}

func TestManagerAppEncryptDecrypt(t *testing.T) {
	key, err := walletcrypto.GenerateEncryptionKey()
	if err != nil {
		t.Fatalf("GenerateEncryptionKey() error = %v", err)
	}

	manager := NewManagerWithEncryption(walletcrypto.KeyPair{}, key)

	err = manager.LoadSource("/brain.js", []byte(`
module.exports = {
	routes: {
		'POST /crypto': function cryptoRoute(data) {
			const encrypted = crypto.encrypt(data.secret);
			return {
				status: 200,
				body: {
					encrypted: encrypted,
					decrypted: crypto.decrypt(encrypted),
				},
			};
		},
	},
};
`))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	route, _, ok := manager.Match("POST", "/crypto")
	if !ok {
		t.Fatal("expected route to be registered")
	}

	result, err := manager.InvokeHandler(route, map[string]any{"secret": "classified"})
	if err != nil {
		t.Fatalf("expected no error invoking handler, got %v", err)
	}

	body, ok := result.Export().(map[string]any)["body"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected body type: %T", result.Export().(map[string]any)["body"])
	}
	if body["encrypted"] == "classified" || body["encrypted"] == "" {
		t.Fatalf("unexpected encrypted value: %#v", body["encrypted"])
	}
	if body["decrypted"] != "classified" {
		t.Fatalf("unexpected decrypted value: %#v", body["decrypted"])
	}
}

func TestManagerAppLLMGeneratesText(t *testing.T) {
	key, err := walletcrypto.GenerateEncryptionKey()
	if err != nil {
		t.Fatalf("GenerateEncryptionKey() error = %v", err)
	}

	fakeLLM := &fakeLLMConnector{text: "llm response"}
	manager := NewManagerWithEncryptionAndLLM(walletcrypto.KeyPair{}, key, fakeLLM)

	err = manager.LoadSource("/brain.js", []byte(`
module.exports = {
	routes: {
		'POST /llm': function llmRoute(data) {
			return {
				status: 200,
				body: {
					response: llm.ask(data.prompt),
				},
			};
		},
	},
};
`))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	route, _, ok := manager.Match("POST", "/llm")
	if !ok {
		t.Fatal("expected route to be registered")
	}

	result, err := manager.InvokeHandler(route, map[string]any{"prompt": "brain prompt"})
	if err != nil {
		t.Fatalf("expected no error invoking handler, got %v", err)
	}

	body, ok := result.Export().(map[string]any)["body"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected body type: %T", result.Export().(map[string]any)["body"])
	}
	if body["response"] != "llm response" {
		t.Fatalf("unexpected llm response: %#v", body["response"])
	}
	if fakeLLM.prompt != "brain prompt" {
		t.Fatalf("unexpected prompt: %q", fakeLLM.prompt)
	}
}

func TestManagerAppLLMReturnsErrorWhenUnconfigured(t *testing.T) {
	manager := NewManager()

	err := manager.LoadSource("/brain.js", []byte(`
module.exports = {
	routes: {
		'POST /llm': function llmRoute(data) {
			return llm.ask(data.prompt);
		},
	},
};
`))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	route, _, ok := manager.Match("POST", "/llm")
	if !ok {
		t.Fatal("expected route to be registered")
	}

	_, err = manager.InvokeHandler(route, map[string]any{"prompt": "brain prompt"})
	if err == nil {
		t.Fatal("expected missing llm connector error")
	}
}

func waitForCounter(t *testing.T, manager Manager, route Route, minimum int64) {
	t.Helper()

	deadline := time.Now().Add(250 * time.Millisecond)
	for time.Now().Before(deadline) {
		result, err := manager.InvokeHandler(route, nil)
		if err != nil {
			t.Fatalf("expected no error invoking handler, got %v", err)
		}

		body, ok := result.Export().(map[string]any)["body"].(map[string]any)
		if !ok {
			t.Fatalf("unexpected body type: %T", result.Export().(map[string]any)["body"])
		}

		if counter, ok := body["counter"].(int64); ok && counter >= minimum {
			return
		}

		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("expected autonomous loop counter to reach %d", minimum)
}
