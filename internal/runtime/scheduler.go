package runtime

import (
	"context"
	"sync"
)

// LayerExecutor executes one named unit.
type LayerExecutor func(ctx context.Context, name string) error

// RunLayers executes topological layers; each layer in parallel, layers sequentially.
func RunLayers(ctx context.Context, layers [][]string, exec LayerExecutor) error {
	for _, layer := range layers {
		var wg sync.WaitGroup
		errCh := make(chan error, len(layer))
		for _, name := range layer {
			wg.Add(1)
			stepName := name
			go func() {
				defer wg.Done()
				if err := exec(ctx, stepName); err != nil {
					errCh <- err
				}
			}()
		}
		wg.Wait()
		close(errCh)
		for err := range errCh {
			if err != nil {
				return err
			}
		}
	}
	return nil
}
