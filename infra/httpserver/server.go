package httpserver

import (
	"encoding/json"
	"fmt"
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
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if result == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if err := writeResponse(w, result.Export()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
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
