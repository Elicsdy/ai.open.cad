package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	releaseDir := "./release"
	if err := os.MkdirAll(releaseDir, 0o755); err != nil {
		panic(err)
	}

	distDir := "./goreleaser-dist"
	files, err := os.ReadDir(distDir)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Printf("%s does not exist, nothing to move\n", distDir)
			return
		}
		panic(err)
	}

	for _, file := range files {
		if filepath.Ext(file.Name()) != ".zip" {
			continue
		}
		oldPath := filepath.Join(distDir, file.Name())
		newPath := filepath.Join(releaseDir, file.Name())
		if err := os.Rename(oldPath, newPath); err != nil {
			panic(err)
		}
	}
}
