package dag

import "fmt"

// Graph is a simple directed graph for step dependencies.
type Graph struct {
	nodes    map[string]struct{}
	edgesOut map[string]map[string]struct{}
	inDegree map[string]int
}

// NewGraph creates empty graph.
func NewGraph() *Graph {
	return &Graph{
		nodes:    map[string]struct{}{},
		edgesOut: map[string]map[string]struct{}{},
		inDegree: map[string]int{},
	}
}

// AddNode inserts a new node.
func (g *Graph) AddNode(name string) error {
	if _, exists := g.nodes[name]; exists {
		return fmt.Errorf("duplicate node %q", name)
	}
	g.nodes[name] = struct{}{}
	g.edgesOut[name] = map[string]struct{}{}
	g.inDegree[name] = 0
	return nil
}

// AddEdge adds dependency edge from -> to.
func (g *Graph) AddEdge(from, to string) error {
	if _, ok := g.nodes[from]; !ok {
		return fmt.Errorf("unknown node %q", from)
	}
	if _, ok := g.nodes[to]; !ok {
		return fmt.Errorf("unknown node %q", to)
	}
	if _, exists := g.edgesOut[from][to]; exists {
		return nil
	}
	g.edgesOut[from][to] = struct{}{}
	g.inDegree[to]++
	return nil
}

// Nodes returns all node names.
func (g *Graph) Nodes() map[string]struct{} {
	out := make(map[string]struct{}, len(g.nodes))
	for n := range g.nodes {
		out[n] = struct{}{}
	}
	return out
}
