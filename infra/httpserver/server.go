package httpserver

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"utobot/infra/brain"
)

type Server struct {
	addr    string
	manager brain.Manager
	server  *http.Server
}

func New(port int, manager brain.Manager) *Server {
	addr := fmt.Sprintf(":%d", port)
	return &Server{
		addr:    addr,
		manager: manager,
	}
}

func (s *Server) Addr() string {
	return s.addr
}

func (s *Server) Start() error {
	s.server = &http.Server{
		Addr:    s.addr,
		Handler: s,
	}

	slog.Info("http server listening", "addr", s.addr)
	return s.server.ListenAndServe()
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.addCORSHeaders(w, r)

	if handled, err := s.handleCORSPreflight(w, r); handled {
		if err != nil {
			writeErrorPage(w, http.StatusInternalServerError, "CORS handling error", err, brain.Route{}, r)
		}
		return
	}

	route, params, ok := s.manager.Match(r.Method, r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}

	payload, err := buildRequestPayload(r, params)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	result, err := s.manager.InvokeHandler(route, payload)
	if err != nil {
		writeErrorPage(w, http.StatusInternalServerError, "Brain script error", err, route, r)
		return
	}

	if result == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if err := writeResponse(w, result.Export()); err != nil {
		writeErrorPage(w, http.StatusInternalServerError, "Response rendering error", err, route, r)
	}
}

func buildRequestPayload(r *http.Request, params map[string]string) (map[string]any, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, fmt.Errorf("read request body: %w", err)
	}

	var parsedJSON any
	if len(body) > 0 {
		if err := json.Unmarshal(body, &parsedJSON); err != nil {
			parsedJSON = nil
		}
	}

	query := make(map[string]string, len(r.URL.Query()))
	for key, values := range r.URL.Query() {
		if len(values) > 0 {
			query[key] = values[0]
		}
	}

	headers := make(map[string]string, len(r.Header))
	for key, values := range r.Header {
		if len(values) > 0 {
			headers[strings.ToLower(key)] = values[0]
		}
	}

	return map[string]any{
		"method":     r.Method,
		"path":       r.URL.Path,
		"params":     params,
		"query":      query,
		"headers":    headers,
		"body":       string(body),
		"json":       parsedJSON,
		"remoteAddr": r.RemoteAddr,
		"requestId":  r.Header.Get("X-Request-Id"),
	}, nil
}

func (s *Server) handleCORSPreflight(w http.ResponseWriter, r *http.Request) (bool, error) {
	if r.Method != http.MethodOptions {
		return false, nil
	}

	requestedMethod := strings.TrimSpace(r.Header.Get("Access-Control-Request-Method"))
	if requestedMethod == "" {
		return false, nil
	}

	route, _, ok := s.manager.Match(strings.ToUpper(requestedMethod), r.URL.Path)
	s.addCORSHeaders(w, r)
	if !ok {
		http.NotFound(w, r)
		return true, nil
	}

	w.Header().Set("Access-Control-Allow-Methods", route.Method)
	if requestedHeaders := strings.TrimSpace(r.Header.Get("Access-Control-Request-Headers")); requestedHeaders != "" {
		w.Header().Set("Access-Control-Allow-Headers", requestedHeaders)
	} else {
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With, X-Request-Id")
	}
	w.Header().Set("Access-Control-Max-Age", "86400")
	w.WriteHeader(http.StatusNoContent)
	return true, nil
}

func (s *Server) addCORSHeaders(w http.ResponseWriter, r *http.Request) {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return
	}

	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Add("Vary", "Origin")
}

func writeResponse(w http.ResponseWriter, exported any) error {
	switch resp := exported.(type) {
	case nil:
		w.WriteHeader(http.StatusNoContent)
		return nil
	case map[string]any:
		return writeStructuredResponse(w, resp)
	case string:
		w.Header().Set("content-type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, err := io.WriteString(w, resp)
		return err
	default:
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(http.StatusOK)
		return json.NewEncoder(w).Encode(resp)
	}
}

func writeStructuredResponse(w http.ResponseWriter, resp map[string]any) error {
	status := http.StatusOK
	if rawStatus, ok := resp["status"]; ok {
		if parsedStatus, ok := toStatusCode(rawStatus); ok {
			status = parsedStatus
		}
	}

	headers := responseHeaders(resp["headers"])
	if _, ok := w.Header()["Content-Type"]; !ok {
		if contentType, ok := resp["contentType"].(string); ok && strings.TrimSpace(contentType) != "" {
			w.Header().Set("content-type", strings.TrimSpace(contentType))
		}
	}
	for key, value := range headers {
		if len(value) > 0 {
			w.Header().Set(key, value[0])
		}
	}

	body := resp["body"]
	if body == nil {
		w.WriteHeader(status)
		return nil
	}

	if _, ok := w.Header()["Content-Type"]; !ok {
		switch body.(type) {
		case string:
			w.Header().Set("content-type", "text/plain; charset=utf-8")
		default:
			w.Header().Set("content-type", "application/json")
		}
	}

	w.WriteHeader(status)
	switch value := body.(type) {
	case string:
		_, err := io.WriteString(w, value)
		return err
	case []byte:
		_, err := w.Write(value)
		return err
	default:
		return json.NewEncoder(w).Encode(value)
	}
}

func responseHeaders(value any) http.Header {
	headers := make(http.Header)

	if typed, ok := value.(map[string]any); ok {
		for key, rawValue := range typed {
			switch v := rawValue.(type) {
			case []string:
				if len(v) > 0 {
					headers.Set(key, v[0])
				}
			default:
				headers.Set(key, fmt.Sprint(v))
			}
		}
	}

	return headers
}

func toStatusCode(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case int8:
		return int(typed), true
	case int16:
		return int(typed), true
	case int32:
		return int(typed), true
	case int64:
		return int(typed), true
	case float32:
		return int(typed), true
	case float64:
		return int(typed), true
	case json.Number:
		parsed, err := strconv.Atoi(typed.String())
		if err == nil {
			return parsed, true
		}
	}

	return 0, false
}

func writeErrorPage(w http.ResponseWriter, status int, title string, err error, route brain.Route, r *http.Request) {
	w.Header().Set("content-type", "text/html; charset=utf-8")
	w.WriteHeader(status)

	data := errorPageData{
		Status:     status,
		StatusText: http.StatusText(status),
		Title:      title,
		Error:      err.Error(),
		Method:     r.Method,
		Path:       r.URL.Path,
		Route:      route.Method + " " + route.Path,
		RequestID:  r.Header.Get("X-Request-Id"),
	}

	if renderErr := errorPageTemplate.Execute(w, data); renderErr != nil {
		slog.Error("failed to render error page", "error", renderErr)
	}
}

type errorPageData struct {
	Status     int
	StatusText string
	Title      string
	Error      string
	Method     string
	Path       string
	Route      string
	RequestID  string
}

var errorPageTemplate = template.Must(template.New("error-page").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{.Status}} {{.StatusText}} - {{.Title}}</title>
  <style>
    :root {
      color-scheme: light dark;
      --bg: #101418;
      --card: #17202a;
      --text: #edf2f7;
      --muted: #9fb1c1;
      --accent: #ffb86b;
      --border: #314253;
    }
    body {
      margin: 0;
      min-height: 100vh;
      background: radial-gradient(circle at top left, #26384a, var(--bg) 42rem);
      color: var(--text);
      font: 15px/1.5 ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", monospace;
    }
    main {
      box-sizing: border-box;
      width: min(980px, calc(100vw - 32px));
      margin: 0 auto;
      padding: 48px 0;
    }
    section {
      background: color-mix(in srgb, var(--card) 92%, transparent);
      border: 1px solid var(--border);
      border-radius: 18px;
      box-shadow: 0 24px 80px rgba(0, 0, 0, .35);
      overflow: hidden;
    }
    header {
      padding: 24px 28px;
      border-bottom: 1px solid var(--border);
    }
    h1 {
      margin: 0;
      color: var(--accent);
      font-size: clamp(24px, 4vw, 40px);
      letter-spacing: -.04em;
    }
    dl {
      display: grid;
      grid-template-columns: max-content 1fr;
      gap: 8px 18px;
      margin: 0;
      padding: 22px 28px;
      border-bottom: 1px solid var(--border);
    }
    dt {
      color: var(--muted);
    }
    dd {
      margin: 0;
      overflow-wrap: anywhere;
    }
    pre {
      margin: 0;
      padding: 24px 28px;
      overflow-x: auto;
      white-space: pre-wrap;
      color: #ffd7ba;
    }
  </style>
</head>
<body>
  <main>
    <section>
      <header>
        <h1>{{.Status}} {{.StatusText}}: {{.Title}}</h1>
      </header>
      <dl>
        <dt>Request</dt><dd>{{.Method}} {{.Path}}</dd>
        <dt>Route</dt><dd>{{.Route}}</dd>
        {{if .RequestID}}<dt>Request ID</dt><dd>{{.RequestID}}</dd>{{end}}
      </dl>
      <pre>{{.Error}}</pre>
    </section>
  </main>
</body>
</html>
`))
