# Utobot

Utobot is a small Go HTTP host that loads a JavaScript brain, serves HTTP routes, and keeps the runtime state on the Go side.

The current code is intentionally simple. It reads startup parameters from JSON, fetches a brain script by chain id, loads that script into `goja`, starts an HTTP server, and reloads the script when the backing chain height changes.

## Run it

```bash
go run . ./config.json
```

`main.go` expects the config path as the first argument. A second non-`--debug` argument overrides the brain chain id. `--debug` raises the log level.

Example:

```bash
go run . ./config.json AZ3BXaotezKx2AioD3gGvA --debug
```

## Docker

Build the container image:

```bash
docker build -t utobot .
```

Run it with a mounted config and wallet directory:

```bash
docker run --rm \
  -p 8081:8081 \
  -v "$PWD/config.json:/etc/utobot/config.json" \
  -v "$PWD/wallets:/wallets" \
  utobot
```

The image runs `utobot /etc/utobot/config.json` by default. Pass another config path, chain id override, or `--debug` as container arguments when needed.

## Config

The config loader expects a flat JSON object with these fields.

| Field | Required | Meaning |
| --- | --- | --- |
| `http_address` | yes | Base address used to fetch the JavaScript brain |
| `port` | yes | Port used by the local HTTP server |
| `wallet_path` | yes | Required by startup validation; currently kept in the loaded parameters |
| `brain_chain_id` | yes | Chain id used to fetch the JavaScript brain |
| `script_height_path` | no | File used by the reload worker to remember the last seen height |
| `encryption_key` | no | Base64-encoded 32-byte AES-256-GCM key; generated and saved on first load when absent |

Example:

```json
{
  "http_address": "127.0.0.1:8080",
  "port": 8081,
  "wallet_path": "./wallet.json",
  "brain_chain_id": "AZ3BXaotezKx2AioD3gGvA",
  "script_height_path": "./.utobot.script-height.json",
  "encryption_key": "base64-encoded-32-byte-key-generated-on-first-load"
}
```

If `script_height_path` is empty, the reload worker falls back to `.utobot.script-height.json`.

## JavaScript Brain

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
  remoteAddr: "127.0.0.1:53012",
  requestId: "..."
}
```

The runtime also exposes two globals:

| Global | Meaning |
| --- | --- |
| `console.log`, `console.warn`, `console.error`, `console.debug` | Forward to the Go logger |
| `app.get_context()`, `app.set_context(value)` | Shared in-memory state for the loaded script |
| `app.configure_loop(delayMs, fn)` | Run `fn` repeatedly in the brain runtime every `delayMs` milliseconds |
| `app.encrypt(plaintext)`, `app.decrypt(payload)` | AES-256-GCM encrypt/decrypt with the configured key |

`app` is useful when the script wants to keep JSON-like state across multiple requests.

```js
app.set_context({ counter: 0 });

module.exports = {
  routes: {
    "POST /count": function (req) {
      const ctx = app.get_context();
      ctx.counter += 1;

      return {
        status: 200,
        body: {
          counter: ctx.counter
        }
      };
    }
  }
};
```

## Autonomous Loop

A brain can configure one autonomous loop at load time. The loop runs inside the same JavaScript runtime as request handlers, so context and module-level variables are shared.

```js
let ticks = 0;

app.configure_loop(5000, function autonomousTick() {
  ticks += 1;
  console.log("autonomous tick", ticks);
});

module.exports = {
  routes: {
    "GET /ticks": function () {
      return { status: 200, body: { ticks } };
    }
  }
};
```

Aliases are available for compatibility: `app.configure_autonomous_loop`, `app.set_loop`, `app.autonomous_loop`, and `app.loop`.

## Encryption

Brains can encrypt and decrypt strings with AES-256-GCM. The returned encrypted payload is base64 containing nonce plus ciphertext.

```js
module.exports = {
  routes: {
    "POST /secret": function (req) {
      const encrypted = app.encrypt(req.body);
      return {
        status: 200,
        body: {
          encrypted,
          decrypted: app.decrypt(encrypted)
        }
      };
    }
  }
};
```

Aliases are also available under `app.cypher.encrypt/decrypt` and `app.cipher.encrypt/decrypt`.

## Responses

Handlers can return a plain string, a plain object, or nothing.

If a handler returns a string, Utobot writes it as plain text. If it returns an object, Utobot understands `status`, `headers`, and `body`. If `body` is an object, it is encoded as JSON. If the handler returns nothing, the server answers with `204 No Content`.

## Reload

Utobot keeps the current brain height in the file given by `script_height_path`. On startup it stores the current height, then a background worker checks the chain periodically and reloads the script when the height changes.


The current tests also show the exact runtime behavior:

```bash
go test ./infra/brain
```
