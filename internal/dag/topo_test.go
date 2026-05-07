package dag

import "testing"

func TestTopologicalLayers(t *testing.T) {
	t.Parallel()
	g := NewGraph()
	for _, node := range []string{"a", "b", "c", "d"} {
		if err := g.AddNode(node); err != nil {
			t.Fatal(err)
		}
	}
	_ = g.AddEdge("a", "c")
	_ = g.AddEdge("b", "c")
	_ = g.AddEdge("c", "d")

	layers, err := TopologicalLayers(g)
	if err != nil {
		t.Fatal(err)
	}
	if len(layers) != 3 {
		t.Fatalf("want 3 layers got %d", len(layers))
	}
}

func TestTopologicalLayersCycle(t *testing.T) {
	t.Parallel()
	g := NewGraph()
	for _, node := range []string{"a", "b"} {
		if err := g.AddNode(node); err != nil {
			t.Fatal(err)
		}
	}
	_ = g.AddEdge("a", "b")
	_ = g.AddEdge("b", "a")

	if _, err := TopologicalLayers(g); err == nil {
		t.Fatalf("expected cycle error")
	}
}
