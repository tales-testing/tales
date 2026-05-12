package embeddeddriver

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Extract writes the contents of fsys rooted at srcRoot into dst. The
// extraction is atomic: files are first written under "<dst>.tmp" and
// then rename(2)'d into place once every file has been copied. Missing
// parent directories of dst are created on the fly.
//
// Path traversal attempts inside fsys are rejected.
func Extract(fsys fs.FS, srcRoot, dst string) error {
	if fsys == nil {
		return fmt.Errorf("extract: filesystem is nil")
	}

	if dst == "" {
		return fmt.Errorf("extract: destination is empty")
	}

	tmp := dst + ".tmp"

	if err := os.RemoveAll(tmp); err != nil {
		return fmt.Errorf("clear tmp dir %q: %w", tmp, err)
	}

	if err := os.MkdirAll(tmp, 0o755); err != nil {
		return fmt.Errorf("mkdir tmp %q: %w", tmp, err)
	}

	walkErr := fs.WalkDir(fsys, srcRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel := strings.TrimPrefix(p, srcRoot)
		rel = strings.TrimPrefix(rel, "/")

		if rel == "" {
			return nil
		}

		cleaned := filepath.Clean(rel)
		if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
			return fmt.Errorf("rejected unsafe path %q", p)
		}

		target := filepath.Join(tmp, cleaned)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}

		return writeFile(fsys, p, target)
	})
	if walkErr != nil {
		_ = os.RemoveAll(tmp)

		return fmt.Errorf("extract: %w", walkErr)
	}

	if err := os.RemoveAll(dst); err != nil {
		_ = os.RemoveAll(tmp)

		return fmt.Errorf("clear destination %q: %w", dst, err)
	}

	if err := os.Rename(tmp, dst); err != nil {
		_ = os.RemoveAll(tmp)

		return fmt.Errorf("rename tmp to dst: %w", err)
	}

	return nil
}

func writeFile(fsys fs.FS, src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("mkdir parent: %w", err)
	}

	in, err := fsys.Open(src)
	if err != nil {
		return fmt.Errorf("open source %q: %w", src, err)
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("open destination %q: %w", dst, err)
	}

	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()

		return fmt.Errorf("copy %q: %w", src, err)
	}

	if err := out.Close(); err != nil {
		return fmt.Errorf("close %q: %w", dst, err)
	}

	return nil
}
