package main

import (
	"testing"
	"utobot/domain/graph"
	"utobot/infra/crypto"
	"utobot/infra/network"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeploy(t *testing.T) {
	b64String := "eyJ2IjoxLCJjdXJ2ZSI6InNlY3AyNTZrMSIsInByaXZfaGV4IjoiYWI2YTg1NWVlNTVhMGU0OGM3Y2U4ZjJhZTQ1N2U0MTZkNjAzMTBhMzU4OWFjNjMxNzIyYmE3OTUzYmY2Yzk4OCIsInB1Yl9oZXgiOiIwNDM4MTQ5M2U2MTg5Y2UxNThhMmZlNjdkZTA3ZjFhYzE3ZTAxY2YwZmRjYWRjNGUxZDJkMmViOWU3ODVmZjNhNmU2Nzk4NWI5MDcyOWQwM2NmZDljZWNjMGQ0YjNiNWJmYWE5ZThmNWI1NmQxMmVhYzRiZTNlMzcwOTE1N2JmMDQzIiwicHViX2NvbXByZXNzZWQiOmZhbHNlLCJhZGRyZXNzIjoiMHgwMGEzZTc1YTg0MDQyNjIxNGUzNTdiMjE5YzBiYjI4YWUxODRhOTAwIiwiY3JlYXRlZF9hdCI6IjIwMjYtMDMtMjJUMjI6MzA6MTUuNzczWiJ9"
	keyPair, err := crypto.LoadFromB64String(b64String)
	require.NoError(t, err)

	g := graph.NewGraphWithID(keyPair.Address)
	g.Root.Set("title", "Hello!")
	serializedGraph, err := g.ToJson()
	require.NoError(t, err)

	httpClient := network.NewHttpClient("https://metapixia.com")

	dr := network.DeployRequest{
		ChainID:       keyPair.Address,
		Kind:          "graph",
		Content:       serializedGraph,
		CommitMessage: "First push from utobot",
	}

	result, err := httpClient.Deploy(dr, keyPair)
	require.NoError(t, err)
	require.Equal(t, true, result.Success)

	lastState, err := httpClient.GetLastState(keyPair.Address)
	require.NoError(t, err)
	assert.Equal(t, "Hello!", lastState.Root.Get("title"))

}
