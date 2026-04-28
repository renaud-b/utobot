package graph

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

type JsonGraph struct {
	ID   string                 `json:"graph_id"`
	Root map[string]interface{} `json:"root"`
}

func (jsonGraph *JsonGraph) ToGraph() (*Graph, error) {
	root, err := ElementFromMap(jsonGraph.Root)
	if err != nil {
		return nil, err
	}
	return &Graph{
		ID:   jsonGraph.ID,
		Root: root,
	}, nil
}

type Graph struct {
	ID   string   `json:"graph_id"`
	Root *Element `json:"root"`
}

func NewGraph() Graph {
	return Graph{
		ID:   uuid.NewString(),
		Root: NewGraphElement("root"),
	}
}

func NewGraphWithID(graphID string) Graph {
	return Graph{
		ID:   graphID,
		Root: NewGraphElement("root"),
	}
}

func NewGraphFromSnapshot(graphID string, snapShot string) (*Graph, error) {
	root, err := ElementFromJson([]byte(snapShot))
	if err != nil {
		return nil, err
	}

	return &Graph{
		ID:   graphID,
		Root: root,
	}, nil
}

func (g *Graph) ToJson() (string, error) {
	out := struct {
		ID   string            `json:"graph_id"`
		Root *JsonGraphElement `json:"root"`
	}{
		ID:   g.ID,
		Root: (*JsonGraphElement)(g.Root),
	}
	jsonBytes, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	return string(jsonBytes), nil
}
func (g *Graph) FindElement(elementID string) (*Element, error) {
	element, err := g.Root.Find(elementID)
	if err != nil {
		return nil, err
	}
	if element == nil {
		return nil, fmt.Errorf("can't  fin element %s", elementID)
	}
	return element, nil
}

func (g *Graph) DeepCopy() Graph {
	newGraph := NewGraphWithID(g.ID)
	newGraph.Root = g.Root.DeepCopy()
	return newGraph
}

func (g *Graph) Checksum() (string, error) {
	jsonStr, err := json.Marshal(g)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(jsonStr)
	return fmt.Sprintf("%x", sum), nil
}
