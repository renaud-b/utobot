package graph

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
)

type JsonGraphElements []Element

type JsonGraphElement Element

func (e JsonGraphElement) MarshalJSON() ([]byte, error) {

	var parent string
	if e.Parent != nil {
		parent = e.Parent.ID
	}
	type data struct {
		ID       string             `json:"id"`
		Name     string             `json:"name"`
		Path     string             `json:"path"`
		Parent   string             `json:"parent,omitempty"`
		Children []JsonGraphElement `json:"children,omitempty"`
	}

	var children []JsonGraphElement
	for _, c := range e.Children {
		element := JsonGraphElement(*c)
		children = append(children, element)
	}

	marshal, err := json.Marshal(&data{
		ID:       e.ID,
		Name:     e.Name,
		Path:     e.Path,
		Parent:   parent,
		Children: children,
	})
	if err != nil {
		return nil, err
	}

	var m map[string]interface{}
	err = json.Unmarshal(marshal, &m)
	if err != nil {
		return nil, err
	}
	for key, value := range e.Properties {
		m[key] = value
	}
	return json.Marshal(m)
}

// ElementFromMap creates a new element from a map
func ElementFromMap(m map[string]interface{}) (*Element, error) {
	name, exists := m["name"]
	if !exists {
		name = "root"
	}

	element := NewGraphElement(name.(string))
	for key, v := range m {
		if key == "children" || key == "name" || key == "parent" || key == "id" || key == "path" {
			continue
		}
		switch value := v.(type) {
		case string:
			element.Set(key, value)
		case int:
			element.Set(key, fmt.Sprintf("%d", value))
		case float64:
			element.Set(key, fmt.Sprintf("%f", value))
		case bool:
			element.Set(key, fmt.Sprintf("%t", value))
		default:
			slog.Warn("unsupported property type for key %s: %T", key, v)
			continue
		}
	}
	children, exists := m["children"]
	if !exists {
		return element, nil
	}
	for _, child := range children.([]interface{}) {
		childElement, err := ElementFromMap(child.(map[string]interface{}))
		if err != nil {
			return nil, err
		}
		element.AddChildElement(childElement)
	}
	return element, nil
}

// ElementFromJson loads a graph from a json string
func ElementFromJson(b []byte) (*Element, error) {
	var rawMap map[string]interface{}
	if err := json.Unmarshal(b, &rawMap); err != nil {
		return nil, err
	}
	return ElementFromMap(rawMap)
}

type Element struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Parent     *Element          `json:"parent,omitempty"`
	Path       string            `json:"path"`
	Children   []*Element        `json:"children,omitempty"`
	Properties map[string]string `json:"properties,omitempty"`
}

func NewGraphElement(name string) *Element {
	g := &Element{
		Name:       name,
		Children:   make([]*Element, 0),
		Properties: make(map[string]string),
	}
	g.GenerateID()
	return g
}

func NewGraphElementWithParent(parent *Element, name string) *Element {
	finalName := name
	if parent != nil {
		// element already exists, rewrite it's name
		if parent.HasNext(name) {
			return newGraphElementWithParentCount(parent, name, 1)
		}
	}
	g := &Element{
		Name:       finalName,
		Parent:     parent,
		Children:   make([]*Element, 0),
		Properties: make(map[string]string),
	}
	g.GenerateID()
	return g
}

func newGraphElementWithParentCount(parent *Element, name string, i int) *Element {
	finalName := fmt.Sprintf("%s_%d", name, i)
	// element already exists, rewrite it's name
	if parent.HasNext(finalName) {
		return newGraphElementWithParentCount(parent, name, i+1)
	}
	g := &Element{
		Name:       finalName,
		Parent:     parent,
		Children:   make([]*Element, 0),
		Properties: make(map[string]string),
	}
	g.GenerateID()
	return g
}

func (e *Element) AddChildWithIDAndName(id, name string) *Element {
	child := NewGraphElementWithParent(e, name)
	child.ID = id
	e.Children = append(e.Children, child)
	return child
}

func (e *Element) ToMap() map[string]interface{} {
	m := make(map[string]interface{})
	m["name"] = e.Name
	m["id"] = e.ID
	m["path"] = e.Path

	// S'assurer que Parent est un type simple ou nul si non défini
	if e.Parent != nil {
		m["parent"] = e.Parent.ID
	} else {
		m["parent"] = nil
	}

	// Copier les propriétés en s'assurant qu'elles sont de types simples
	for k, v := range e.Properties {
		m[k] = v // Vérifier que v est un type simple ou le convertir
	}

	// Convertir les enfants récursivement
	var children []interface{}
	for _, child := range e.Children {
		children = append(children, child.ToMap())
	}
	m["children"] = children

	return m
}

func (e *Element) HasParent() bool {
	return e.Parent != nil
}

func (e *Element) HasChildren() bool {
	if e == nil {
		return false
	}
	return len(e.Children) > 0
}
func (e *Element) ClearChildren() {
	for _, child := range e.Children {
		e.RemoveChild(child)
	}
}

func (e *Element) GenerateID() {
	e.Path = e.buildPath()
	hash := md5.Sum([]byte(e.Path))
	elementID := hex.EncodeToString(hash[:])
	e.ID = elementID

	if e.HasChildren() {
		for _, child := range e.Children {
			child.GenerateID()
		}
	}

}

func (e *Element) buildPath() string {
	var path []rune
	if !e.HasParent() {
		path = []rune(fmt.Sprintf("//%s", e.Name))
	} else {
		path = append(path, '/')
		var pathElements []string
		pathElements = append(pathElements, e.Name)

		parent := e.Parent
		for parent != nil {
			pathElements = append(pathElements, parent.Name)
			parent = parent.Parent
		}

		for i, j := 0, len(pathElements)-1; i < j; i, j = i+1, j-1 {
			pathElements[i], pathElements[j] = pathElements[j], pathElements[i]
		}

		for _, name := range pathElements {
			path = append(path, []rune(fmt.Sprintf("/%s", name))...)
		}
	}
	return string(path)

}

func (e *Element) Find(targetID string) (*Element, error) {
	if e.ID == targetID {
		return e, nil
	}
	if len(e.Children) > 0 {
		for _, child := range e.Children {
			element, err := child.Find(targetID)
			if err == nil && element != nil {
				return element, nil
			}
		}
	}
	return nil, fmt.Errorf("no such element %s", targetID)
}

func (e *Element) Traverse(f func(element *Element) bool) bool {
	r := f(e)
	if !r {
		return r
	}
	if e.HasChildren() {
		for _, child := range e.Children {
			r = child.Traverse(f)
			if !r {
				return r
			}
		}
	}
	return true
}

func (e *Element) setNameWithIndex(name string, index int) {
	expectedName := fmt.Sprintf("%s_%d", name, index)
	if e.Parent == nil || !e.Parent.HasNext(expectedName) {
		e.Name = fmt.Sprintf("%s_%d", name, index)
		if e.HasParent() {
			e.Parent.GenerateID()
		} else {
			e.GenerateID()
		}
		return
	}
	e.setNameWithIndex(name, index+1)
}

func (e *Element) Set(property string, value string) *Element {
	if property == "children" {
		e.AddChild(value)
		return e
	}
	if property == "name" {
		// element already exists, rewrite it's name
		if e.Parent == nil || !e.Parent.HasNext(value) {
			e.Name = value
			e.GenerateID()
			return e
		}
		e.setNameWithIndex(value, 1)
		return e
	}
	e.Properties[property] = value
	return e
}

func (e *Element) Get(property string) string {
	return e.GetOrDefault(property, "")
}

func (e *Element) GetInt(property string, defaultValue int) int {
	if !e.Has(property) {
		return defaultValue
	}

	value := e.Get(property)
	if value == "" {
		return defaultValue
	}

	var result int
	_, err := fmt.Sscanf(value, "%d", &result)
	if err != nil {
		return defaultValue
	}

	return result
}

func (e *Element) GetFloat(property string, defaultValue float64) float64 {
	if !e.Has(property) {
		return defaultValue
	}

	value := e.Get(property)
	if value == "" {
		return defaultValue
	}

	var result float64

	_, err := fmt.Sscanf(value, "%f", &result)
	if err != nil {
		return defaultValue
	}

	return result
}

func (e *Element) GetOrDefault(property string, defaultValue string) string {
	if e.Properties == nil {
		return defaultValue
	}

	prop, ok := e.Properties[property]
	if !ok {
		return defaultValue
	}
	if prop == "" {
		return defaultValue
	}
	return prop
}

func (e *Element) RemoveParent() {
	if e.Parent != nil {
		output := make([]*Element, 0)
		for _, c := range e.Parent.Children {
			if c.ID != e.ID {
				output = append(output, c)
			}
		}
		e.Parent.Children = output
		e.Parent = nil
	}
}
func (e *Element) FindValidChildName(childName string) string {
	if !e.HasNext(childName) {
		return childName
	}
	for i := 1; e.HasNext(childName); i++ {
		childName = fmt.Sprintf("%s_%d", childName, i)
	}
	return childName
}

func (e *Element) AddChildElement(child *Element) *Element {
	child.Name = e.FindValidChildName(child.Name)
	child.RemoveParent()

	e.Children = append(e.Children, child)
	child.Parent = e
	child.Parent.GenerateID()
	return e
}

func (e *Element) AddChild(value string) *Element {
	child := NewGraphElementWithParent(e, value)
	produceType := e.Get("produceType")
	if produceType != "" {
		child.Set("type", produceType)
	}
	e.Children = append(e.Children, child)
	return child
}

func (e *Element) Next(value string) (*Element, error) {
	for _, child := range e.Children {
		if child.Name == value {
			return child, nil
		}
	}
	return nil, fmt.Errorf("[%s] no such element : %s", e.Path, value)
}

func (e *Element) NextWithProp(key, value string) (*Element, bool) {
	for _, child := range e.Children {
		if key == "name" && child.Name == value {
			return child, true
		} else if child.Has(key) && child.Get(key) == value {
			return child, true
		}
	}
	return nil, false
}

func (e *Element) HasNext(value string) bool {
	for _, child := range e.Children {
		if child.Name == value {
			return true
		}
	}
	return false
}

func (e *Element) DeepCopy() *Element {
	element := NewGraphElement(e.Name)
	for k, v := range e.Properties {
		element.Set(k, v)
	}

	for _, c := range e.Children {
		newChild := c.DeepCopy()
		element.AddChildElement(newChild)
	}
	element.GenerateID()
	return element
}

func (e *Element) ReplaceWith(element *Element) bool {
	p := e.Parent
	if p == nil {
		return false
	}
	found := false
	for i, c := range p.Children {
		if c.ID == e.ID {
			p.Children[i] = element
			found = true
			break
		}
	}
	if !found {
		return found
	}
	e.Parent = nil
	element.Parent = p
	return true
}

func (e *Element) Has(key string) bool {
	_, ok := e.Properties[key]
	return ok
}

func (e *Element) HasNotEmpty(key string) bool {
	value, ok := e.Properties[key]
	return ok && len(value) > 0
}

func (e *Element) ToJson() (string, error) {
	j, err := json.Marshal(JsonGraphElement(*e))
	if err != nil {
		return "", err
	}
	return string(j), nil
}

func (e *Element) String() string {
	return fmt.Sprintf("%s:%s", e.ID, e.Name)
}

func (e *Element) Delete() {
	if e.Parent != nil {
		e.Parent.RemoveChild(e)
	}
}

func (e *Element) RemoveChild(e2 *Element) {
	for i, c := range e.Children {
		if c.ID == e2.ID {
			e.Children = append(e.Children[:i], e.Children[i+1:]...)
			e2.Parent = nil
			break
		}
	}
}

func (e *Element) Root() *Element {
	if e.Parent == nil {
		return e
	}
	return e.Parent.Root()
}

func (e *Element) Equals(other *Element) bool {
	if e.Name != other.Name ||
		e.Path != other.Path ||
		len(e.Properties) != len(other.Properties) ||
		len(e.Children) != len(other.Children) {
		return false
	}
	for k, v := range e.Properties {
		if other.Get(k) != v {
			return false
		}
	}
	for i, c := range e.Children {
		if !c.Equals(other.Children[i]) {
			return false
		}
	}
	return true
}

func (e *Element) HasSameStructure(other *Element) bool {
	if len(e.Children) != len(other.Children) {
		return false
	}
	for i, c := range e.Children {
		if !c.HasSameStructure(other.Children[i]) {
			return false
		}
	}
	return true
}
