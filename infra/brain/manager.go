package brain

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"utobot/infra/crypto"

	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/require"
)

type Manager interface {
	LoadSource(path string, source []byte) error
	Routes() map[string]Route
	Match(method, path string) (Route, map[string]string, bool)
	InvokeHandler(route Route, data any) (goja.Value, error)
	Path() string
}

type Route struct {
	Method  string
	Path    string
	Handler goja.Callable
}
type JsManager struct {
	mu         sync.RWMutex
	wallet     crypto.KeyPair
	cipherKey  []byte
	llm        LLMConnector
	runtime    *goja.Runtime
	routes     map[string]Route
	path       string
	loopCancel context.CancelFunc
	loopDone   chan struct{}
}

type LLMConnector interface {
	GenerateText(ctx context.Context, prompt string) (string, error)
}

type autonomousLoop struct {
	delay time.Duration
	fn    goja.Callable
}

func NewManager(wallet ...crypto.KeyPair) Manager {
	var keyPair crypto.KeyPair
	if len(wallet) > 0 {
		keyPair = wallet[0]
	}

	cipherKey, err := crypto.GenerateEncryptionKey()
	if err != nil {
		slog.Warn("failed to generate transient brain encryption key", "error", err)
	}

	return &JsManager{wallet: keyPair, cipherKey: cipherKey}
}

func NewManagerWithEncryption(wallet crypto.KeyPair, cipherKey []byte) Manager {
	keyCopy := append([]byte(nil), cipherKey...)
	return &JsManager{wallet: wallet, cipherKey: keyCopy}
}

func NewManagerWithEncryptionAndLLM(wallet crypto.KeyPair, cipherKey []byte, llm LLMConnector) Manager {
	keyCopy := append([]byte(nil), cipherKey...)
	return &JsManager{wallet: wallet, cipherKey: keyCopy, llm: llm}
}

func (m *JsManager) LoadSource(path string, source []byte) error {
	if path == "" {
		return fmt.Errorf("brain path is required")
	}

	m.stopAutonomousLoop()

	cleanPath := filepath.Clean(path)

	runtime := goja.New()
	var loop *autonomousLoop

	registry := require.NewRegistry()
	registry.Enable(runtime)

	module := runtime.NewObject()
	exports := runtime.NewObject()

	if err := module.Set("exports", exports); err != nil {
		return fmt.Errorf("create module.exports: %w", err)
	}
	if err := runtime.Set("module", module); err != nil {
		return fmt.Errorf("set module: %w", err)
	}
	if err := runtime.Set("exports", exports); err != nil {
		return fmt.Errorf("set exports: %w", err)
	}
	if err := bindConsole(runtime); err != nil {
		return fmt.Errorf("bind console: %w", err)
	}
	if err := bindFetch(runtime); err != nil {
		return fmt.Errorf("bind fetch: %w", err)
	}
	if err := bindApp(runtime, m.wallet, m.cipherKey, m.llm, &loop); err != nil {
		return fmt.Errorf("bind app: %w", err)
	}

	if _, err := runtime.RunScript(cleanPath, string(source)); err != nil {
		return fmt.Errorf("load brain %s: %w", cleanPath, err)
	}

	moduleVal := runtime.Get("module")
	if moduleVal == nil || goja.IsUndefined(moduleVal) || goja.IsNull(moduleVal) {
		return fmt.Errorf("brain %s did not define module", cleanPath)
	}

	moduleObj := moduleVal.ToObject(runtime)
	exportsVal := moduleObj.Get("exports")

	if exportsVal == nil || goja.IsUndefined(exportsVal) || goja.IsNull(exportsVal) {
		return fmt.Errorf("brain %s does not export anything", cleanPath)
	}

	exportsObj := exportsVal.ToObject(runtime)
	routesVal := exportsObj.Get("routes")

	if routesVal == nil || goja.IsUndefined(routesVal) || goja.IsNull(routesVal) {
		return fmt.Errorf("brain %s does not export routes", cleanPath)
	}

	routesObj := routesVal.ToObject(runtime)
	routes := make(map[string]Route)

	for _, key := range routesObj.Keys() {
		value := routesObj.Get(key)

		fn, ok := goja.AssertFunction(value)
		if !ok {
			return fmt.Errorf("route %q is not a function", key)
		}

		parts := strings.SplitN(key, " ", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid route format %q, expected 'METHOD /path'", key)
		}

		method := strings.ToUpper(strings.TrimSpace(parts[0]))
		routePath := strings.TrimSpace(parts[1])

		if method == "" {
			return fmt.Errorf("route %q has empty method", key)
		}
		if routePath == "" || !strings.HasPrefix(routePath, "/") {
			return fmt.Errorf("route %q has invalid path", key)
		}

		routeKey := method + " " + routePath

		routes[routeKey] = Route{
			Method:  method,
			Path:    routePath,
			Handler: fn,
		}
	}

	m.mu.Lock()
	m.runtime = runtime
	m.routes = routes
	m.path = cleanPath
	m.mu.Unlock()

	if loop != nil {
		m.startAutonomousLoop(cleanPath, runtime, loop)
	}

	return nil
}

func (m *JsManager) Routes() map[string]Route {
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make(map[string]Route, len(m.routes))
	for k, v := range m.routes {
		out[k] = v
	}
	return out
}

func (m *JsManager) Match(method, path string) (Route, map[string]string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	key := method + " " + path
	if route, ok := m.routes[key]; ok {
		return route, nil, true
	}

	for routeKey, route := range m.routes {
		routeMethod, routePattern, ok := strings.Cut(routeKey, " ")
		if !ok || routeMethod != method {
			continue
		}

		params, matched := matchRoutePattern(routePattern, path)
		if matched {
			return route, params, true
		}
	}

	return Route{}, nil, false
}

func (m *JsManager) InvokeHandler(route Route, data any) (goja.Value, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.runtime == nil {
		return nil, fmt.Errorf("brain runtime is not initialized")
	}

	// Convert Go → JS
	jsData := m.runtime.ToValue(data)

	// Call JS handler
	result, err := route.Handler(goja.Undefined(), jsData)
	if err != nil {
		return nil, fmt.Errorf("invoke handler %s %s: %w", route.Method, route.Path, err)
	}

	return result, nil
}

func (m *JsManager) Path() string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.path
}

func (m *JsManager) startAutonomousLoop(path string, runtime *goja.Runtime, loop *autonomousLoop) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	m.mu.Lock()
	m.loopCancel = cancel
	m.loopDone = done
	m.mu.Unlock()

	go func() {
		defer close(done)

		ticker := time.NewTicker(loop.delay)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.mu.Lock()
				if ctx.Err() != nil || m.runtime != runtime {
					m.mu.Unlock()
					return
				}

				_, err := loop.fn(goja.Undefined())
				m.mu.Unlock()

				if err != nil {
					slog.Warn("autonomous brain loop failed", "brain_path", path, "error", err)
				}
			}
		}
	}()
}

func (m *JsManager) stopAutonomousLoop() {
	m.mu.Lock()
	cancel := m.loopCancel
	done := m.loopDone
	m.loopCancel = nil
	m.loopDone = nil
	m.mu.Unlock()

	if cancel == nil {
		return
	}

	cancel()
	if done != nil {
		<-done
	}
}

func bindConsole(runtime *goja.Runtime) error {
	console := runtime.NewObject()
	for name, level := range map[string]slog.Level{
		"debug": slog.LevelDebug,
		"log":   slog.LevelInfo,
		"warn":  slog.LevelWarn,
		"error": slog.LevelError,
	} {
		methodName := name
		methodLevel := level
		if err := console.Set(methodName, func(call goja.FunctionCall) goja.Value {
			args := make([]any, 0, len(call.Arguments))
			for _, arg := range call.Arguments {
				args = append(args, arg.Export())
			}

			slog.Log(context.Background(), methodLevel, "console."+methodName, "args", args)
			return goja.Undefined()
		}); err != nil {
			return err
		}
	}

	return runtime.Set("console", console)
}

func bindApp(runtime *goja.Runtime, wallet crypto.KeyPair, cipherKey []byte, llm LLMConnector, loop **autonomousLoop) error {
	app := runtime.NewObject()
	var contextValue goja.Value = runtime.NewObject()

	/*
		stateChainAddr := "state@" + wallet.Address
		client := network.NewHttpClient("https://metapixia.com")
	*/
	if err := app.Set("get_context", func(goja.FunctionCall) goja.Value {
		/*
			g, err := client.GetLastState(stateChainAddr)
			if err != nil {
				slog.Warn("failed to fetch last state for context", "error", err)
				return contextValue
			}

			contextValue = runtime.ToValue(g)

		*/
		return contextValue
	}); err != nil {
		return err
	}

	if err := app.Set("set_context", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			contextValue = runtime.NewObject()
			return contextValue
		}

		contextValue = call.Argument(0)

		return contextValue
	}); err != nil {
		return err
	}

	configureLoop := func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 2 {
			panic(runtime.NewTypeError("autonomous loop requires delay_ms and function"))
		}

		delayMs := call.Argument(0).ToInteger()
		if delayMs <= 0 {
			panic(runtime.NewTypeError("autonomous loop delay_ms must be greater than 0"))
		}

		fn, ok := goja.AssertFunction(call.Argument(1))
		if !ok {
			panic(runtime.NewTypeError("autonomous loop function must be callable"))
		}

		*loop = &autonomousLoop{
			delay: time.Duration(delayMs) * time.Millisecond,
			fn:    fn,
		}

		return goja.Undefined()
	}

	if err := app.Set("configure_loop", configureLoop); err != nil {
		return err
	}

	encrypt := func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 1 {
			panic(runtime.NewTypeError("encrypt requires plaintext"))
		}

		encrypted, err := crypto.EncryptAES256GCM(cipherKey, call.Argument(0).String())
		if err != nil {
			panic(runtime.NewGoError(err))
		}

		return runtime.ToValue(encrypted)
	}

	decrypt := func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 1 {
			panic(runtime.NewTypeError("decrypt requires encrypted payload"))
		}

		decrypted, err := crypto.DecryptAES256GCM(cipherKey, call.Argument(0).String())
		if err != nil {
			panic(runtime.NewGoError(err))
		}

		return runtime.ToValue(decrypted)
	}

	if err := app.Set("encrypt", encrypt); err != nil {
		return err
	}
	if err := app.Set("decrypt", decrypt); err != nil {
		return err
	}

	generateText := func(call goja.FunctionCall) goja.Value {
		if llm == nil {
			panic(runtime.NewGoError(fmt.Errorf("llm connector is not configured")))
		}
		if len(call.Arguments) < 1 {
			panic(runtime.NewTypeError("llm requires prompt"))
		}

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		text, err := llm.GenerateText(ctx, call.Argument(0).String())
		if err != nil {
			panic(runtime.NewGoError(err))
		}

		return runtime.ToValue(text)
	}

	if err := app.Set("llm", generateText); err != nil {
		return err
	}

	return runtime.Set("app", app)
}

func matchRoutePattern(pattern, path string) (map[string]string, bool) {
	patternParts := splitPath(pattern)
	pathParts := splitPath(path)

	if len(patternParts) != len(pathParts) {
		return nil, false
	}

	params := make(map[string]string)
	for i := range patternParts {
		patternPart := patternParts[i]
		pathPart := pathParts[i]

		if strings.HasPrefix(patternPart, ":") {
			key := strings.TrimPrefix(patternPart, ":")
			if key == "" {
				return nil, false
			}
			params[key] = pathPart
			continue
		}

		if patternPart != pathPart {
			return nil, false
		}
	}

	return params, true
}

func splitPath(value string) []string {
	trimmed := strings.Trim(value, "/")
	if trimmed == "" {
		return []string{}
	}

	return strings.Split(trimmed, "/")
}
