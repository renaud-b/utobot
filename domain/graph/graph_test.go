package graph

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewGraphFromSnapshot(t *testing.T) {
	graphID := uuid.New()
	tmpBlock := `{"children":[{"email":"Renaud-b@utopixia.com","id":"a0492ceacd08b32c8dd4efa216e01ef5","message":"New message!!","name":"1ff92ad2-9128-57c3-852d-76e1fe3aea48","object":"Test objet","parent":"6cabd64da56756ff81e5eeb628cdf375","path":"//root/1ff92ad2-9128-57c3-852d-76e1fe3aea48","userName":"Renaud"},{"clientIP":"92.184.112.14","email":"kevinmemel1@gmail.com","id":"a486934db3094cfaa6ff48db5869b013","message":"testos","name":"b212b118-0ecf-5370-a620-ed7d12ec7928","object":"TestPourHombre","parent":"6cabd64da56756ff81e5eeb628cdf375","path":"//root/b212b118-0ecf-5370-a620-ed7d12ec7928","userName":"MemKev"}],"graphName":"O - Contact thread","id":"6cabd64da56756ff81e5eeb628cdf375","labels":"ororea.com","name":"root","path":"//root"}`
	g, err := NewGraphFromSnapshot(graphID.String(), tmpBlock)
	require.NoError(t, err)
	assert.NotNil(t, g)

	assert.Equal(t, graphID.String(), g.ID)
	assert.Equal(t, "O - Contact thread", g.Root.Get("graphName"))
}

func TestGraphToJson(t *testing.T) {
	g := NewGraph()
	g.Root.Set("graphName", "O - Contact thread")
	jsonStr, err := g.ToJson()
	require.NoError(t, err)
	assert.JSONEq(t, `{"graph_id":"`+g.ID+`","root":{"id":"6cabd64da56756ff81e5eeb628cdf375","name":"root","path":"//root","graphName":"O - Contact thread"}}`, jsonStr)

	decodedJsonGraph := JsonGraph{}
	err = json.Unmarshal([]byte(jsonStr), &decodedJsonGraph)
	require.NoError(t, err)

	decodedG, err := decodedJsonGraph.ToGraph()
	require.NoError(t, err)

	assert.Equal(t, g.ID, decodedG.ID)
	prop := decodedG.Root.Get("graphName")
	
	assert.Equal(t, "O - Contact thread", prop)
}
