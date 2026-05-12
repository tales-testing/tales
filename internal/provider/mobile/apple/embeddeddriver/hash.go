package embeddeddriver

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"sort"
	"strings"
)

// Hash returns a deterministic SHA-256 over every file in fsys rooted
// at root. Both path and content participate in the digest so renames
// and edits flip the result. File mode and timestamps are ignored.
func Hash(fsys fs.FS, root string) (string, error) {
	if fsys == nil {
		return "", fmt.Errorf("hash: filesystem is nil")
	}

	var files []string

	err := fs.WalkDir(fsys, root, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if d.IsDir() {
			return nil
		}

		files = append(files, p)

		return nil
	})
	if err != nil {
		return "", fmt.Errorf("walk %q: %w", root, err)
	}

	sort.Strings(files)

	h := sha256.New()

	for _, p := range files {
		rel := strings.TrimPrefix(p, root)
		rel = strings.TrimPrefix(rel, "/")

		if _, err := h.Write([]byte(rel)); err != nil {
			return "", fmt.Errorf("hash path %q: %w", p, err)
		}

		if _, err := h.Write([]byte{0}); err != nil {
			return "", fmt.Errorf("hash separator %q: %w", p, err)
		}

		if err := mixFileContents(h, fsys, p); err != nil {
			return "", err
		}

		if _, err := h.Write([]byte{0}); err != nil {
			return "", fmt.Errorf("hash trailer %q: %w", p, err)
		}
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

func mixFileContents(h io.Writer, fsys fs.FS, p string) error {
	f, err := fsys.Open(p)
	if err != nil {
		return fmt.Errorf("open %q: %w", p, err)
	}
	defer f.Close()

	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("hash %q: %w", p, err)
	}

	return nil
}
