package network

import (
	"encoding/json"
	"fmt"
	"net/http"
)

func (h *HttpClient) GetLastBlockIndex(chainID string) (int, error) {
	url := h.url + "/_p/8080/chain/" + chainID + "/last"
	resp, err := h.client.Get(url)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	// Check response status code
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("failed to fetch last block index: status code %d", resp.StatusCode)
	}

	var result struct {
		Index int `json:"index"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("decode response body: %w", err)
	}

	return result.Index, nil
}

func (m *Mock) GetLastBlockIndex(chainID string) (int, error) {
	args := m.Called(chainID)
	return args.Int(0), args.Error(1)
}
