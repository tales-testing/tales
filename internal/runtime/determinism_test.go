package runtime

import (
	"fmt"
	"sync"
	"testing"
)

func TestParallelOrderDoesNotChangeGeneratedValues(t *testing.T) {
	t.Parallel()
	baseSeed := int64(1234)
	parts := []string{"a", "b", "c", "d", "e", "f"}

	run := func(reverse bool) map[string]int {
		results := map[string]int{}
		mu := sync.Mutex{}
		var wg sync.WaitGroup
		for i := 0; i < len(parts); i++ {
			idx := i
			if reverse {
				idx = len(parts) - 1 - i
			}
			part := parts[idx]
			wg.Add(1)
			go func(p string) {
				defer wg.Done()
				r := NewDeterministicRand(baseSeed, "scenario", "step", p)
				value := r.Intn(1000000)
				mu.Lock()
				results[p] = value
				mu.Unlock()
			}(part)
		}
		wg.Wait()
		return results
	}

	a := run(false)
	b := run(true)
	for _, part := range parts {
		if a[part] != b[part] {
			t.Fatalf("value changed for %s: %d vs %d", part, a[part], b[part])
		}
	}

	// Small smoke check to ensure values are not all equal.
	if a["a"] == a["b"] && a["b"] == a["c"] {
		t.Fatalf("unexpected equal values: %v", fmt.Sprint(a))
	}
}
