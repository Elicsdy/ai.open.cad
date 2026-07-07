package main

import (
	"os"
	"path/filepath"
	"testing"
)

func withTempCWD(t *testing.T) string {
	t.Helper()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}
	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir temp failed: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(wd)
	})
	return tmp
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}
}

func TestMainMovesZipAndKeepsNonZip(t *testing.T) {
	root := withTempCWD(t)
	writeFile(t, filepath.Join(root, "goreleaser-dist", "a.zip"), []byte("zip"))
	writeFile(t, filepath.Join(root, "goreleaser-dist", "note.txt"), []byte("txt"))

	main()

	if _, err := os.Stat(filepath.Join(root, "release", "a.zip")); err != nil {
		t.Fatalf("zip file should be moved to release: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "goreleaser-dist", "a.zip")); !os.IsNotExist(err) {
		t.Fatalf("zip file should no longer exist in dist")
	}
	if _, err := os.Stat(filepath.Join(root, "goreleaser-dist", "note.txt")); err != nil {
		t.Fatalf("non-zip file should stay in dist: %v", err)
	}
}

func TestMainReturnsWhenDistMissing(t *testing.T) {
	withTempCWD(t)

	main()
}

func TestMainPanicsWhenRenameFails(t *testing.T) {
	root := withTempCWD(t)
	writeFile(t, filepath.Join(root, "goreleaser-dist", "a.zip"), []byte("from-dist"))
	if err := os.MkdirAll(filepath.Join(root, "release", "a.zip"), 0o755); err != nil {
		t.Fatalf("mkdir release/a.zip failed: %v", err)
	}

	defer func() {
		if recover() == nil {
			t.Fatalf("expected panic when rename fails")
		}
	}()

	main()
}
