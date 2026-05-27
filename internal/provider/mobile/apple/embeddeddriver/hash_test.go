package embeddeddriver

import (
	"testing"
	"testing/fstest"
)

func TestHashDeterministic(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{
		"src/a.txt":     {Data: []byte("alpha")},
		"src/b.txt":     {Data: []byte("beta")},
		"src/sub/c.txt": {Data: []byte("gamma")},
	}

	first, err := Hash(fsys, "src")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}

	second, err := Hash(fsys, "src")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}

	if first != second {
		t.Fatalf("hash is not deterministic: %s != %s", first, second)
	}
}

func TestHashSensitiveToContent(t *testing.T) {
	t.Parallel()

	base := fstest.MapFS{
		"src/a.txt": {Data: []byte("alpha")},
	}
	mutated := fstest.MapFS{
		"src/a.txt": {Data: []byte("alphaX")},
	}

	a, err := Hash(base, "src")
	if err != nil {
		t.Fatalf("hash base: %v", err)
	}

	b, err := Hash(mutated, "src")
	if err != nil {
		t.Fatalf("hash mutated: %v", err)
	}

	if a == b {
		t.Fatalf("hash should differ when content changes (%s)", a)
	}
}

func TestHashSensitiveToPath(t *testing.T) {
	t.Parallel()

	base := fstest.MapFS{
		"src/a.txt": {Data: []byte("payload")},
	}
	renamed := fstest.MapFS{
		"src/b.txt": {Data: []byte("payload")},
	}

	a, err := Hash(base, "src")
	if err != nil {
		t.Fatalf("hash base: %v", err)
	}

	b, err := Hash(renamed, "src")
	if err != nil {
		t.Fatalf("hash renamed: %v", err)
	}

	if a == b {
		t.Fatalf("hash should differ when path changes (%s)", a)
	}
}

func TestHashStableAcrossInsertionOrder(t *testing.T) {
	t.Parallel()

	first := fstest.MapFS{
		"src/a.txt": {Data: []byte("1")},
		"src/b.txt": {Data: []byte("2")},
		"src/c.txt": {Data: []byte("3")},
	}
	second := fstest.MapFS{
		"src/c.txt": {Data: []byte("3")},
		"src/a.txt": {Data: []byte("1")},
		"src/b.txt": {Data: []byte("2")},
	}

	a, err := Hash(first, "src")
	if err != nil {
		t.Fatalf("hash first: %v", err)
	}

	b, err := Hash(second, "src")
	if err != nil {
		t.Fatalf("hash second: %v", err)
	}

	if a != b {
		t.Fatalf("hash should be order-independent: %s != %s", a, b)
	}
}

func TestHashNilFilesystem(t *testing.T) {
	t.Parallel()

	if _, err := Hash(nil, "anywhere"); err == nil {
		t.Fatalf("expected error on nil filesystem")
	}
}
