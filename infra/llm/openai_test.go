package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAIConnectorGenerateText(t *testing.T) {
	var gotAuth string
	var gotRequest responsesRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}

		gotAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotRequest); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		_ = json.NewEncoder(w).Encode(responsesResponse{
			ID:         "resp_1",
			OutputText: "hello from openai",
		})
	}))
	defer server.Close()

	connector, err := NewOpenAIConnector("sk-test", WithOpenAIBaseURL(server.URL), WithOpenAIModel("gpt-test"))
	if err != nil {
		t.Fatalf("NewOpenAIConnector() error = %v", err)
	}

	text, err := connector.GenerateText(context.Background(), "hello")
	if err != nil {
		t.Fatalf("GenerateText() error = %v", err)
	}

	if text != "hello from openai" {
		t.Fatalf("unexpected text: %q", text)
	}
	if gotAuth != "Bearer sk-test" {
		t.Fatalf("unexpected authorization header: %q", gotAuth)
	}
	if gotRequest != (responsesRequest{Model: "gpt-test", Input: "hello"}) {
		t.Fatalf("unexpected request: %#v", gotRequest)
	}
}

func TestOpenAIConnectorGenerateTextReadsOutputContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(responsesResponse{
			ID: "resp_1",
			Output: []responsesItem{
				{
					Type: "message",
					Content: []responsesContent{
						{Type: "output_text", Text: "nested text"},
					},
				},
			},
		})
	}))
	defer server.Close()

	connector, err := NewOpenAIConnector("sk-test", WithOpenAIBaseURL(server.URL))
	if err != nil {
		t.Fatalf("NewOpenAIConnector() error = %v", err)
	}

	text, err := connector.GenerateText(context.Background(), "hello")
	if err != nil {
		t.Fatalf("GenerateText() error = %v", err)
	}
	if text != "nested text" {
		t.Fatalf("unexpected text: %q", text)
	}
}

func TestOpenAIConnectorRejectsMissingKey(t *testing.T) {
	_, err := NewOpenAIConnector("")
	if err == nil {
		t.Fatal("expected missing key error")
	}
}

func TestOpenAIConnectorReturnsAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad key", http.StatusUnauthorized)
	}))
	defer server.Close()

	connector, err := NewOpenAIConnector("sk-test", WithOpenAIBaseURL(server.URL))
	if err != nil {
		t.Fatalf("NewOpenAIConnector() error = %v", err)
	}

	_, err = connector.GenerateText(context.Background(), "hello")
	if err == nil {
		t.Fatal("expected API error")
	}
}
