package network

import (
	"encoding/json"
	"fmt"
	"utobot/domain/graph"
)

func (h *HttpClient) GetLastState(chainID string) (*graph.Graph, error) {
	url := h.url + "/_p/8080/chain/" + chainID + "/state"
	resp, err := h.client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Check response status code
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("failed to fetch last state: status code %d", resp.StatusCode)
	}
	// Sample payload retrieve from the state route:
	// {"graph_data":{"graph_id":"0x00a3e75a840426214e357b219c0bb28ae184a900","root":{"children":[],"id":"6cabd64da56756ff81e5eeb628cdf375","name":"root","parent":"","path":"//root"}},"last_block_index":4}
	// We need to parse graph.Graph from graph_data field in the response
	var result struct {
		GraphData graph.JsonGraph `json:"graph_data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response body: %w", err)
	}
	g, err := result.GraphData.ToGraph()
	if err != nil {
		return nil, fmt.Errorf("convert json graph to graph: %w", err)
	}

	return g, nil
}

func (m *Mock) GetLastState(chainID string) (*graph.Graph, error) {
	args := m.Called(chainID)
	return args.Get(0).(*graph.Graph), args.Error(1)
}
