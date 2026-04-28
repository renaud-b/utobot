package graph

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestElement_GetFloat(t *testing.T) {
	root := NewGraphElement("root")
	root.Set("float", "20.0")

	assert.Equal(t, 20.0, root.GetFloat("float", 0.0))

}

func TestElement_Equals(t *testing.T) {
	root1 := NewGraphElement("root")
	root2 := NewGraphElement("root")
	assert.True(t, root1.Equals(root2))

	child1 := root1.AddChild("child")
	child2 := root2.AddChild("child")
	assert.True(t, root1.Equals(root2))

	child1.Set("property", "value")
	assert.False(t, root1.Equals(root2))
	child2.Set("property", "value")
	assert.True(t, root1.Equals(root2))

}

func TestElement_AddChild(t *testing.T) {
	root := NewGraphElement("root")
	root.AddChild("child")
	assert.Equal(t, 1, len(root.Children))
	root.AddChild("child")
	assert.Equal(t, 2, len(root.Children))

	assert.True(t, root.HasNext("child"))
	assert.True(t, root.HasNext("child_1"))
}

func TestElement_AddChildElement(t *testing.T) {
	root := NewGraphElement("root")
	child1 := NewGraphElement("child")
	root.AddChildElement(child1)
	assert.Equal(t, 1, len(root.Children))

	child2 := NewGraphElement("child")
	root.AddChildElement(child2)
	assert.Equal(t, 2, len(root.Children))

	assert.True(t, root.HasNext("child"))
	assert.True(t, root.HasNext("child_1"))
}

func TestElement_Delete(t *testing.T) {
	root := NewGraphElement("root")
	child := root.AddChild("child")
	child_1 := child.AddChild("child1")

	child2 := root.AddChild("child2")
	_ = child2.AddChild("child1")

	assert.True(t, child.HasNext("child1"))
	child_1.Delete()
	r, err := root.Next("child")
	require.NoError(t, err)

	assert.False(t, r.HasNext("child1"))
}
