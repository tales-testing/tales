//go:build windows

package embeddeddriver

// acquireFileLock is a no-op on Windows. The Apple driver only runs on
// macOS at runtime; Windows builds exist solely so the package compiles
// in cross-platform CI.
func acquireFileLock(_ string) (func(), error) {
	return func() {}, nil
}
