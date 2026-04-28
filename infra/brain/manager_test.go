package brain

import (
	"testing"
	"time"

	walletcrypto "utobot/infra/crypto"
)

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
app.set_context({ counter: 0, tags: [] });

module.exports = {
	routes: {
		'POST /state': function state(data) {
			const ctx = app.get_context();
			ctx.counter += 1;
			ctx.tags.push(data.tag);

			return {
				status: 200,
				body: {
					counter: ctx.counter,
					tags: app.get_context().tags,
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

func TestManagerAutonomousLoopRunsConfiguredFunction(t *testing.T) {
	manager := NewManager()

	err := manager.LoadSource("/brain.js", []byte(`
let counter = 0;

app.configure_loop(5, function autonomousTick() {
	counter += 1;
});

module.exports = {
	routes: {
		'GET /counter': function counterRoute() {
			return {
				status: 200,
				body: { counter: counter },
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
let counter = 0;

app.loop(5, function autonomousTick() {
	counter += 1;
});

module.exports = {
	routes: {
		'GET /counter': function counterRoute() {
			return { status: 200, body: { version: 1, counter: counter } };
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
let counter = 0;

module.exports = {
	routes: {
		'GET /counter': function counterRoute() {
			return { status: 200, body: { version: 2, counter: counter } };
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
	if got := body["counter"]; got != int64(0) {
		t.Fatalf("expected second brain without loop to keep counter at 0, got %#v", got)
	}
}

func TestManagerAutonomousLoopRejectsInvalidConfiguration(t *testing.T) {
	manager := NewManager()

	err := manager.LoadSource("/brain.js", []byte(`
app.configure_loop(0, function () {});

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
			const encrypted = app.encrypt(data.secret);
			return {
				status: 200,
				body: {
					encrypted: encrypted,
					decrypted: app.decrypt(encrypted),
					cypherDecrypted: app.cypher.decrypt(app.cypher.encrypt(data.secret)),
					cipherDecrypted: app.cipher.decrypt(app.cipher.encrypt(data.secret)),
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
	if body["cypherDecrypted"] != "classified" {
		t.Fatalf("unexpected cypher decrypted value: %#v", body["cypherDecrypted"])
	}
	if body["cipherDecrypted"] != "classified" {
		t.Fatalf("unexpected cipher decrypted value: %#v", body["cipherDecrypted"])
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
