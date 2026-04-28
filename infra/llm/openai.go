package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const (
	DefaultOpenAIBaseURL = "https://api.openai.com/v1"
	DefaultOpenAIModel   = "gpt-5.4-mini"
)

type OpenAIConnector struct {
	apiKey     string
	baseURL    string
	model      string
	httpClient *http.Client
}

type OpenAIOption func(*OpenAIConnector)

func NewOpenAIConnector(apiKey string, options ...OpenAIOption) (*OpenAIConnector, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("openai llm key is required")
	}

	connector := &OpenAIConnector{
		apiKey:     apiKey,
		baseURL:    DefaultOpenAIBaseURL,
		model:      DefaultOpenAIModel,
		httpClient: http.DefaultClient,
	}

	for _, option := range options {
		option(connector)
	}

	connector.baseURL = strings.TrimRight(connector.baseURL, "/")
	if connector.baseURL == "" {
		return nil, fmt.Errorf("openai base URL is required")
	}
	if strings.TrimSpace(connector.model) == "" {
		return nil, fmt.Errorf("openai model is required")
	}
	if connector.httpClient == nil {
		return nil, fmt.Errorf("openai HTTP client is required")
	}

	return connector, nil
}

func WithOpenAIBaseURL(baseURL string) OpenAIOption {
	return func(connector *OpenAIConnector) {
		connector.baseURL = baseURL
	}
}

func WithOpenAIModel(model string) OpenAIOption {
	return func(connector *OpenAIConnector) {
		connector.model = model
	}
}

func WithHTTPClient(client *http.Client) OpenAIOption {
	return func(connector *OpenAIConnector) {
		connector.httpClient = client
	}
}

func (c *OpenAIConnector) SetModel(model string) {
	c.model = model
}

func (c *OpenAIConnector) GenerateText(ctx context.Context, prompt string) (string, error) {
	if strings.TrimSpace(prompt) == "" {
		return "", fmt.Errorf("prompt is required")
	}

	requestBody := responsesRequest{
		Model: c.model,
		Input: prompt,
	}

	body, err := json.Marshal(requestBody)
	if err != nil {
		return "", fmt.Errorf("marshal OpenAI request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/responses", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create OpenAI request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("call OpenAI responses API: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read OpenAI response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("OpenAI responses API returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var response responsesResponse
	if err := json.Unmarshal(respBody, &response); err != nil {
		return "", fmt.Errorf("decode OpenAI response: %w", err)
	}

	if response.OutputText != "" {
		return response.OutputText, nil
	}

	for _, item := range response.Output {
		if item.Type != "message" {
			continue
		}
		for _, content := range item.Content {
			if content.Type == "output_text" && content.Text != "" {
				return content.Text, nil
			}
		}
	}

	return "", fmt.Errorf("OpenAI response did not contain output text")
}

type responsesRequest struct {
	Model string `json:"model"`
	Input string `json:"input"`
}

type responsesResponse struct {
	ID         string          `json:"id"`
	OutputText string          `json:"output_text"`
	Output     []responsesItem `json:"output"`
}

type responsesItem struct {
	Type    string             `json:"type"`
	Content []responsesContent `json:"content"`
}

type responsesContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}
