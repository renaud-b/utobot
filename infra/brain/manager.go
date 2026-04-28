package brain

import (
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"utobot/infra/crypto"
	"utobot/infra/state"

	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/require"
	"github.com/google/uuid"
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
	stateStore state.ContextStore
	source     []byte
	routes     map[string]Route
	path       string
	loopCancel context.CancelFunc
	loopDone   chan struct{}
	generation uint64
}

type LLMConnector interface {
	GenerateText(ctx context.Context, prompt string) (string, error)
	SetModel(model string)
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

	return &JsManager{wallet: keyPair, cipherKey: cipherKey, stateStore: state.NewMemoryStore()}
}

func NewManagerWithEncryption(wallet crypto.KeyPair, cipherKey []byte) Manager {
	keyCopy := append([]byte(nil), cipherKey...)
	return &JsManager{wallet: wallet, cipherKey: keyCopy, stateStore: state.NewMemoryStore()}
}

func NewManagerWithEncryptionAndLLM(wallet crypto.KeyPair, cipherKey []byte, llm LLMConnector) Manager {
	return NewManagerWithStateStore(wallet, cipherKey, llm, nil)
}

func NewManagerWithStateStore(wallet crypto.KeyPair, cipherKey []byte, llm LLMConnector, stateStore state.ContextStore) Manager {
	keyCopy := append([]byte(nil), cipherKey...)
	if stateStore == nil {
		stateStore = state.NewMemoryStore()
	}
	return &JsManager{wallet: wallet, cipherKey: keyCopy, llm: llm, stateStore: stateStore}
}

func (m *JsManager) LoadSource(path string, source []byte) error {
	if path == "" {
		return fmt.Errorf("brain path is required")
	}

	m.stopAutonomousLoop()

	cleanPath := filepath.Clean(path)
	sourceCopy := append([]byte(nil), source...)

	_, routes, loop, err := m.instantiateRuntime(sourceCopy)
	if err != nil {
		return err
	}

	m.mu.Lock()
	m.source = sourceCopy
	m.routes = routes
	m.path = cleanPath
	m.generation++
	generation := m.generation
	m.mu.Unlock()

	if loop != nil {
		m.startAutonomousLoop(cleanPath, generation, loop)
	}

	return nil
}

func (m *JsManager) instantiateRuntime(source []byte) (*goja.Runtime, map[string]Route, *autonomousLoop, error) {
	runtime := goja.New()
	var loop *autonomousLoop

	registry := require.NewRegistry()
	registry.Enable(runtime)

	module := runtime.NewObject()
	exports := runtime.NewObject()

	if err := module.Set("exports", exports); err != nil {
		return nil, nil, nil, fmt.Errorf("create module.exports: %w", err)
	}
	if err := runtime.Set("module", module); err != nil {
		return nil, nil, nil, fmt.Errorf("set module: %w", err)
	}
	if err := runtime.Set("exports", exports); err != nil {
		return nil, nil, nil, fmt.Errorf("set exports: %w", err)
	}
	if err := bindConsole(runtime); err != nil {
		return nil, nil, nil, fmt.Errorf("bind console: %w", err)
	}
	if err := bindFetch(runtime); err != nil {
		return nil, nil, nil, fmt.Errorf("bind fetch: %w", err)
	}
	if err := bindBrainAPI(runtime, m.cipherKey, m.llm, m.stateStore, &loop); err != nil {
		return nil, nil, nil, fmt.Errorf("bind app: %w", err)
	}

	if _, err := runtime.RunScript("brain.js", string(source)); err != nil {
		return nil, nil, nil, fmt.Errorf("load brain: %w", err)
	}

	moduleVal := runtime.Get("module")
	if moduleVal == nil || goja.IsUndefined(moduleVal) || goja.IsNull(moduleVal) {
		return nil, nil, nil, fmt.Errorf("brain did not define module")
	}

	moduleObj := moduleVal.ToObject(runtime)
	exportsVal := moduleObj.Get("exports")

	if exportsVal == nil || goja.IsUndefined(exportsVal) || goja.IsNull(exportsVal) {
		return nil, nil, nil, fmt.Errorf("brain does not export anything")
	}

	exportsObj := exportsVal.ToObject(runtime)
	routesVal := exportsObj.Get("routes")

	if routesVal == nil || goja.IsUndefined(routesVal) || goja.IsNull(routesVal) {
		return nil, nil, nil, fmt.Errorf("brain does not export routes")
	}

	routesObj := routesVal.ToObject(runtime)
	routes := make(map[string]Route)

	for _, key := range routesObj.Keys() {
		value := routesObj.Get(key)

		fn, ok := goja.AssertFunction(value)
		if !ok {
			return nil, nil, nil, fmt.Errorf("route %q is not a function", key)
		}

		parts := strings.SplitN(key, " ", 2)
		if len(parts) != 2 {
			return nil, nil, nil, fmt.Errorf("invalid route format %q, expected 'METHOD /path'", key)
		}

		method := strings.ToUpper(strings.TrimSpace(parts[0]))
		routePath := strings.TrimSpace(parts[1])

		if method == "" {
			return nil, nil, nil, fmt.Errorf("route %q has empty method", key)
		}
		if routePath == "" || !strings.HasPrefix(routePath, "/") {
			return nil, nil, nil, fmt.Errorf("route %q has invalid path", key)
		}

		routeKey := method + " " + routePath

		routes[routeKey] = Route{
			Method:  method,
			Path:    routePath,
			Handler: fn,
		}
	}

	return runtime, routes, loop, nil
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
	m.mu.RLock()
	source := append([]byte(nil), m.source...)
	m.mu.RUnlock()

	if len(source) == 0 {
		return nil, fmt.Errorf("brain runtime is not initialized")
	}

	return m.invokeRouteOnSource(source, route, data)
}

func (m *JsManager) invokeRouteOnSource(source []byte, route Route, data any) (goja.Value, error) {
	runtime, _, _, err := m.instantiateRuntime(source)
	if err != nil {
		return nil, err
	}

	jsData := runtime.ToValue(data)

	routesVal := runtime.Get("module").ToObject(runtime).Get("exports")
	if routesVal == nil || goja.IsUndefined(routesVal) || goja.IsNull(routesVal) {
		return nil, fmt.Errorf("brain does not export anything")
	}

	routesObj := routesVal.ToObject(runtime).Get("routes")
	if routesObj == nil || goja.IsUndefined(routesObj) || goja.IsNull(routesObj) {
		return nil, fmt.Errorf("brain does not export routes")
	}

	routeObj := routesObj.ToObject(runtime)
	handlerVal := routeObj.Get(route.Method + " " + route.Path)
	handler, ok := goja.AssertFunction(handlerVal)
	if !ok {
		return nil, fmt.Errorf("route %s %s is not a function", route.Method, route.Path)
	}

	result, err := handler(goja.Undefined(), jsData)
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

func (m *JsManager) startAutonomousLoop(path string, generation uint64, loop *autonomousLoop) {
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
				m.mu.RLock()
				currentGeneration := m.generation
				m.mu.RUnlock()
				if ctx.Err() != nil || currentGeneration != generation {
					return
				}

				_, err := loop.fn(goja.Undefined())

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

func bindBrainAPI(runtime *goja.Runtime, cipherKey []byte, llm LLMConnector, stateStore state.ContextStore, loop **autonomousLoop) error {
	contextValue, err := loadContextValue(runtime, stateStore)
	if err != nil {
		return err
	}

	persistContext := func(value goja.Value) error {
		if stateStore == nil {
			return nil
		}

		encoded, err := json.Marshal(value.Export())
		if err != nil {
			return fmt.Errorf("encode brain context: %w", err)
		}

		return stateStore.SaveContext(encoded)
	}

	decodeStoredContext := func(raw []byte) (goja.Value, error) {
		value, err := decodeContextValue(runtime, raw)
		if err != nil {
			return nil, err
		}
		return value, nil
	}

	stateObj := runtime.NewObject()
	cryptoObj := runtime.NewObject()
	llmObj := runtime.NewObject()
	app := runtime.NewObject()

	getContext := func(goja.FunctionCall) goja.Value {
		return contextValue
	}

	setContext := func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			contextValue = runtime.NewObject()
		} else {
			contextValue = call.Argument(0)
		}

		if err := persistContext(contextValue); err != nil {
			panic(runtime.NewGoError(err))
		}

		return contextValue
	}

	updateContext := func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 1 {
			panic(runtime.NewTypeError("state.update requires a function"))
		}

		updater, ok := goja.AssertFunction(call.Argument(0))
		if !ok {
			panic(runtime.NewTypeError("state.update function must be callable"))
		}

		runUpdate := func(raw []byte) (goja.Value, error) {
			current, err := decodeStoredContext(raw)
			if err != nil {
				return nil, err
			}

			next, err := updater(goja.Undefined(), current)
			if err != nil {
				return nil, err
			}
			if goja.IsUndefined(next) || goja.IsNull(next) {
				next = current
			}
			return next, nil
		}

		if stateStore == nil {
			next, err := runUpdate(nil)
			if err != nil {
				panic(runtime.NewGoError(err))
			}
			contextValue = next
			return contextValue
		}

		var nextValue goja.Value
		err := stateStore.UpdateContext(func(raw []byte) ([]byte, error) {
			next, err := runUpdate(raw)
			if err != nil {
				return nil, err
			}
			nextValue = next
			encoded, err := json.Marshal(next.Export())
			if err != nil {
				return nil, fmt.Errorf("encode brain context: %w", err)
			}
			return encoded, nil
		})
		if err != nil {
			panic(runtime.NewGoError(err))
		}
		contextValue = nextValue
		return contextValue
	}

	if err := stateObj.Set("get", getContext); err != nil {
		return err
	}
	if err := stateObj.Set("set", setContext); err != nil {
		return err
	}
	if err := stateObj.Set("update", updateContext); err != nil {
		return err
	}
	if err := runtime.Set("state", stateObj); err != nil {
		return err
	}

	loopFn := func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 2 {
			panic(runtime.NewTypeError("loop requires delay_ms and function"))
		}

		delayMs := call.Argument(0).ToInteger()
		if delayMs <= 0 {
			panic(runtime.NewTypeError("loop delay_ms must be greater than 0"))
		}

		fn, ok := goja.AssertFunction(call.Argument(1))
		if !ok {
			panic(runtime.NewTypeError("loop function must be callable"))
		}

		*loop = &autonomousLoop{
			delay: time.Duration(delayMs) * time.Millisecond,
			fn:    fn,
		}

		return goja.Undefined()
	}

	if err := app.Set("loop", loopFn); err != nil {
		return err
	}

	newUUIDFn := func(call goja.FunctionCall) goja.Value {
		return runtime.ToValue(uuid.NewString())
	}

	if err := app.Set("new_uuid", newUUIDFn); err != nil {
		return err
	}

	md5Fn := func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 1 {
			panic(runtime.NewTypeError("md5 requires input string"))
		}

		hash := md5.Sum([]byte(call.Argument(0).String()))
		return runtime.ToValue(fmt.Sprintf("%x", hash))
	}

	if err := app.Set("md5", md5Fn); err != nil {
		return err
	}

	encrypt := func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 1 {
			panic(runtime.NewTypeError("crypto.encrypt requires plaintext"))
		}

		encrypted, err := crypto.EncryptAES256GCM(cipherKey, call.Argument(0).String())
		if err != nil {
			panic(runtime.NewGoError(err))
		}

		return runtime.ToValue(encrypted)
	}

	decrypt := func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 1 {
			panic(runtime.NewTypeError("crypto.decrypt requires encrypted payload"))
		}

		decrypted, err := crypto.DecryptAES256GCM(cipherKey, call.Argument(0).String())
		if err != nil {
			panic(runtime.NewGoError(err))
		}

		return runtime.ToValue(decrypted)
	}

	verifyECDSASignature := func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 3 {
			panic(runtime.NewTypeError("crypto.verify_ecdsa_signature requires public_key_hex, message_hash_hex, and signature_hex"))
		}

		ok, err := crypto.VerifySignature(
			call.Argument(0).String(),
			call.Argument(1).String(),
			call.Argument(2).String(),
		)
		if err != nil {
			panic(runtime.NewGoError(err))
		}

		return runtime.ToValue(ok)
	}

	deriveAddress := func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 1 {
			panic(runtime.NewTypeError("crypto.derive_address requires public_key_hex"))
		}

		return runtime.ToValue(crypto.DeriveAddress(call.Argument(0).String()))
	}

	if err := cryptoObj.Set("encrypt", encrypt); err != nil {
		return err
	}
	if err := cryptoObj.Set("decrypt", decrypt); err != nil {
		return err
	}
	if err := cryptoObj.Set("verify_ecdsa_signature", verifyECDSASignature); err != nil {
		return err
	}
	if err := cryptoObj.Set("derive_address", deriveAddress); err != nil {
		return err
	}
	if err := runtime.Set("crypto", cryptoObj); err != nil {
		return err
	}

	ask := func(call goja.FunctionCall) goja.Value {
		if llm == nil {
			panic(runtime.NewGoError(fmt.Errorf("llm connector is not configured")))
		}
		if len(call.Arguments) < 1 {
			panic(runtime.NewTypeError("llm.ask requires prompt"))
		}

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		if len(call.Arguments) >= 2 {
			model := call.Argument(1).String()
			if model != "" {
				llm.SetModel(model)
			}
		}

		text, err := llm.GenerateText(ctx, call.Argument(0).String())
		if err != nil {
			panic(runtime.NewGoError(err))
		}

		return runtime.ToValue(text)
	}

	if err := llmObj.Set("ask", ask); err != nil {
		return err
	}
	if err := runtime.Set("llm", llmObj); err != nil {
		return err
	}

	if err := runtime.Set("app", app); err != nil {
		return err
	}

	return nil
}

func loadContextValue(runtime *goja.Runtime, stateStore state.ContextStore) (goja.Value, error) {
	if stateStore == nil {
		return runtime.NewObject(), nil
	}

	data, ok, err := stateStore.LoadContext()
	if err != nil {
		return nil, err
	}
	if !ok || len(data) == 0 {
		return runtime.NewObject(), nil
	}

	var decoded any
	if err := json.Unmarshal(data, &decoded); err != nil {
		return nil, fmt.Errorf("decode persisted brain context: %w", err)
	}

	if decoded == nil {
		return runtime.NewObject(), nil
	}

	return runtime.ToValue(decoded), nil
}

func decodeContextValue(runtime *goja.Runtime, raw []byte) (goja.Value, error) {
	if len(raw) == 0 {
		return runtime.NewObject(), nil
	}

	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("decode persisted brain context: %w", err)
	}

	if decoded == nil {
		return runtime.NewObject(), nil
	}

	return runtime.ToValue(decoded), nil
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
