package dag

import "fmt"

// TopologicalLayers returns execution levels where each level can run in parallel.
func TopologicalLayers(g *Graph) ([][]string, error) {
	remainingIn := map[string]int{}
	for name, degree := range g.inDegree {
		remainingIn[name] = degree
	}

	ready := make([]string, 0)

	for name, degree := range remainingIn {
		if degree == 0 {
			ready = append(ready, name)
		}
	}

	layers := make([][]string, 0)
	processed := 0

	for len(ready) > 0 {
		current := append([]string(nil), ready...)
		ready = ready[:0]

		layers = append(layers, current)
		for _, node := range current {
			processed++

			for out := range g.edgesOut[node] {
				remainingIn[out]--
				if remainingIn[out] == 0 {
					ready = append(ready, out)
				}
			}
		}
	}

	if processed != len(g.nodes) {
		return nil, fmt.Errorf("dependency cycle detected")
	}

	return layers, nil
}
