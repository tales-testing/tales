package browser

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// resolveUploadPaths turns the authored upload_file sources into absolute
// paths pointing at existing regular files.
//
// Relative entries are joined to the directory of the .tales file declaring
// the step, matching the HTTP provider's multipart `file { path = ... }`
// resolution so fixtures live next to the suite. The result is always made
// absolute: DOM.setFileInputFiles takes host paths and silently attaches
// nothing for a relative one, which the suite would only notice as a mute
// page (`tales test ./e2e/browser` yields a relative Step.File).
//
// Every entry is stat'd up front: a missing or non-regular file fails the
// action explicitly rather than reaching Chrome and surfacing as an opaque
// CDP error.
func resolveUploadPaths(stepFile string, paths []string) ([]string, error) {
	if len(paths) == 0 {
		return nil, errors.New("upload_file: paths must list at least one file")
	}

	resolved := make([]string, 0, len(paths))

	for _, path := range paths {
		abs, err := resolveUploadPath(stepFile, path)
		if err != nil {
			return nil, err
		}

		info, err := os.Stat(abs)
		if err != nil {
			return nil, fmt.Errorf("upload_file: %q: %w", path, err)
		}

		if info.IsDir() {
			return nil, fmt.Errorf("upload_file: %q is a directory, not a file", path)
		}

		resolved = append(resolved, abs)
	}

	return resolved, nil
}

func resolveUploadPath(stepFile, path string) (string, error) {
	if path == "" {
		return "", errors.New("upload_file: path is empty")
	}

	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}

	if stepFile == "" {
		return "", fmt.Errorf("upload_file: cannot resolve relative path %q: step file is unknown", path)
	}

	abs, err := filepath.Abs(filepath.Join(filepath.Dir(stepFile), path))
	if err != nil {
		return "", fmt.Errorf("upload_file: cannot resolve %q: %w", path, err)
	}

	return abs, nil
}
