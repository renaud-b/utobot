package network

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
)

// FetchJavascript is the method that will be called by the service.
func (h *HttpClient) FetchJavascript(chainID string) ([]byte, error) {
	url := h.url + "/render/" + chainID
	slog.Info("fetching javascript from URL", "url", url)
	resp, err := h.client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Check response status code
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch javascript: status code %d", resp.StatusCode)
	}

	// Check response content type (we need javascript)
	if contentType := resp.Header.Get("Content-Type"); contentType != "application/javascript" {
		return nil, fmt.Errorf("unexpected content type: %s", contentType)
	}

	// Retrieve body as []byte
	return io.ReadAll(resp.Body)
}

// FetchJavascript is the method that will be called by the service. It is defined on both the HttpClient and the Mock,
func (m *Mock) FetchJavascript(chainID string) ([]byte, error) {
	args := m.Called(chainID)
	return args.Get(0).([]byte), args.Error(1)
}
