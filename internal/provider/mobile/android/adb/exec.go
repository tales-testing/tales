package adb

import "os/exec"

// defaultLookPath is the production PATH lookup. It is assigned to the
// execLookPath variable so tests can stub binary discovery without
// touching the real PATH.
func defaultLookPath(name string) (string, bool) {
	path, err := exec.LookPath(name)
	if err != nil {
		return "", false
	}

	return path, true
}
