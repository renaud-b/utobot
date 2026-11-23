# Utobot

Utobot is a small Go HTTP host that loads a JavaScript runtime, serves HTTP routes, and keeps the runtime state in SQLite on the Go side.

The current code is intentionally simple. It reads startup parameters from JSON, fetches a runtime script by chain id, loads that script into `goja`, starts an HTTP server, and reloads the script when the backing chain height changes.

## Run it

```bash
go run . ./config.json
```

`main.go` expects the config path as the first argument. A second non-`--debug` argument overrides the runtime chain id. `--debug` raises the log level.

Example:

```bash
go run . ./config.json AZ3BXaotezKx2AioD3gGvA --debug
```

## Docker

Build the container image:

```bash
docker build -t utobot .
```

Run a walletless bot with a mounted configuration:

```bash
docker run --rm \
  -p 9000:9000 \
  -v "$PWD/config.json:/opt/utobot/config.json" \
  utobot
```

The image runs `utobot /opt/utobot/config.json` by default. The config file
must be writable on first launch if `encryption_key` is not already set.
For durable state, mount the SQLite directory as well and set `state_db_path`
inside it. Ensure the container's `utobot` user can write to those paths.

Bots that need a wallet can still set `wallet_path` to
`/opt/utobot/wallet.utx` and mount the corresponding file with
`-v "$PWD/wallet.utx:/opt/utobot/wallet.utx:ro"`.
A configured wallet must exist and be valid; Utobot never creates one.

Pass another config path, chain id override, or `--debug` as container arguments when needed.

## Config

The config loader expects a flat JSON object with these fields.

| Field | Required | Meaning |
| --- | --- | --- |
| `http_address` | yes | Base address used to fetch the JavaScript runtime |
| `port` | yes | Port used by the local HTTP server |
| `wallet_path` | no | Optional wallet file. If supplied it must exist and be valid; otherwise the runtime starts without an identity |
| `runtime_chain_id` | yes | Chain id used to fetch the JavaScript runtime |
| `state_db_path` | no | SQLite file used to persist the bot state; if omitted, state stays in memory |
| `script_height_path` | no | File used by the reload worker to remember the last seen height |
| `encryption_key` | no | Base64-encoded 32-byte AES-256-GCM key; generated and saved on first load when absent |
| `llm_key` | no | OpenAI API key used by the LLM connector; if omitted, `llm.ask()` is unavailable |

Example:

```json
{
  "http_address": "127.0.0.1:8080",
  "port": 8081,
  "runtime_chain_id": "AZ3BXaotezKx2AioD3gGvA",
  "script_height_path": "./.utobot.script-height.json",
  "encryption_key": "base64-encoded-32-byte-key-generated-on-first-load",
  "llm_key": "sk-your-openai-api-key"
}
```

Omit `wallet_path` to run a service that only verifies signatures and stores state,
such as a friend acceptance inbox. Existing wallet-backed configurations work unchanged.

If `script_height_path` is empty, the reload worker falls back to `.utobot.script-height.json`.
If `state_db_path` is empty or omitted, Utobot keeps state in memory instead of SQLite.

## JavaScript Runtime

The loaded script is a CommonJS module. It must export a `routes` object, where each key is `METHOD /path`.

Path parameters use `:name` segments.

```js

function contact(req) {
    return {
        status: 200,
        body: {
            id: req.params.id,
            email: req.query.email,
        }
    }
}

module.exports = {
    routes: {
        "GET /contact/:id": contact,
    }
}
```

Each handler receives one argument: a request payload built by Go. It looks like this:

```js
{
  method: "POST",
  path: "/contact",
  params: {
    id: "42"
  },
  query: {
    source: "landing"
  },
  headers: {
    "content-type": "application/json"
  },
  body: "...raw body...",
  json: { /* parsed JSON or null */ },
  form: { /* parsed urlencoded form fields or null */ },
  remoteAddr: "127.0.0.1:53012",
  requestId: "..."
}
```

The runtime also exposes `console` plus the following globals:

| Global                                                          | Meaning |
|-----------------------------------------------------------------| --- |
| `console.log`, `console.warn`, `console.error`, `console.debug` | Forward to the Go logger |
| `atob(base64)` | Decode a base64 string to a binary string |
| `btoa(binaryString)` | Encode a binary string to base64 |
| `state.get()`, `state.set(value)`                               | Persisted JSON-like state for the loaded script |
| `state.update(fn)`                                              | Atomically transform the persisted state in SQLite |
| `app.loop(delayMs, fn)`                                         | Run `fn` repeatedly in the JavaScript runtime every `delayMs` milliseconds |
| `app.new_uuid()`                                                | Generate a new UUID v4 string |
| `app.md5(text)`                                                   | Get the MD5 hash of a string |
| `fetch(url, init)`                                                | Synchronous HTTP fetch with the configured timeout and JSON body support |
| `crypto.encrypt(plaintext)`, `crypto.decrypt(payload)`          | AES-256-GCM encrypt/decrypt with the configured key |
| `crypto.verify_ecdsa_signature(publicKeyHex, messageHashHex, signatureHex)` | Verify an ECDSA signature over a hex-encoded hash; accepts compact `r||s` hex and ASN.1 DER |
| `crypto.derive_address(publicKeyHex)`                           | Derive the bot address from a hex-encoded public key |
| `llm.ask(prompt)`                                               | Generate text with the configured OpenAI connector |

Avoid mutating shared state at module load time; initialize it inside a route or loop with `state.update(...)` or `state.set(...)`.

```js
module.exports = {
  routes: {
    "POST /count": function (req) {
      state.update(function (ctx) {
        ctx.counter = (ctx.counter || 0) + 1;
        return ctx;
      });

      return {
        status: 200,
        body: {
          counter: state.get().counter
        }
      };
    }
  }
};
```

## Autonomous Loop

A runtime can configure one autonomous loop at load time. The loop runs in its own runtime, so shared state should go through `state` rather than module-level variables.

```js
app.loop(5000, function autonomousTick() {
  state.update(function (ctx) {
    ctx.ticks = (ctx.ticks || 0) + 1;
    return ctx;
  });
  console.log("autonomous tick", state.get().ticks);
});

module.exports = {
  routes: {
    "GET /ticks": function () {
      return { status: 200, body: { ticks: state.get().ticks || 0 } };
    }
  }
};
```

## Encryption

The runtime can encrypt and decrypt strings with AES-256-GCM. The returned encrypted payload is base64 containing nonce plus ciphertext.

```js
module.exports = {
  routes: {
    "POST /secret": function (req) {
      const encrypted = crypto.encrypt(req.body);
      return {
        status: 200,
        body: {
          encrypted,
          decrypted: crypto.decrypt(encrypted)
        }
      };
    }
  }
};
```

```js
module.exports = {
  routes: {
    "POST /verify": function (req) {
      return {
        status: 200,
        body: {
          valid: crypto.verify_ecdsa_signature(req.json.publicKeyHex, req.json.messageHashHex, req.json.signatureHex),
          address: crypto.derive_address(req.json.publicKeyHex)
        }
      };
    }
  }
};
```

## LLM

When `llm_key` is configured, the runtime can invoke the OpenAI connector synchronously from JavaScript.

```js
module.exports = {
  routes: {
    "POST /ask": function (req) {
      return {
        status: 200,
        body: {
          answer: llm.ask(req.json.prompt)
        }
      };
    }
  }
};
```

## Fetch

The runtime can call outbound HTTP endpoints with a synchronous `fetch(url, init)` helper.

The returned response object includes:

| Property | Meaning |
| --- | --- |
| `status` | HTTP status code |
| `ok` | `true` for 2xx responses |
| `headers` | Response headers as a plain object |
| `body` | Raw response body as text |
| `text()` | Returns the raw body as text |
| `json()` | Parses the body as JSON |

```js
module.exports = {
  routes: {
    "GET /remote": function () {
      const response = fetch("https://example.com/api/data", {
        method: "POST",
        headers: { "X-Trace": "runtime" },
        body: { query: "hello" }
      });

      return {
        status: response.status,
        body: {
          ok: response.ok,
          data: response.json()
        }
      };
    }
  }
};
```

## Responses

Handlers can return a plain string, a plain object, or nothing.

If a handler returns a string, Utobot writes it as plain text. If it returns an object, Utobot understands `status`, `contentType`, `headers`, and `body`. If `body` is an object, it is encoded as JSON. If the handler returns nothing, the server answers with `204 No Content`.

Utobot also handles browser CORS preflight requests automatically for defined routes. If the browser sends an `Origin` header, the server returns `Access-Control-Allow-Origin: *` on the response. If you need credentialed CORS or a restricted origin list, that should be configured in the server rather than in the JavaScript runtime script.

## Reload

Utobot keeps the current runtime height in the file given by `script_height_path`. On startup it stores the current height, then a background worker checks the chain periodically and reloads the script when the height changes.


The current tests also show the exact runtime behavior:

```bash
go test ./infra/runtime
```
