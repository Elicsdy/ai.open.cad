package main

import (
	"encoding/json"
	"flag"
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

func resetFlags(t *testing.T, args ...string) {
	t.Helper()
	oldArgs := os.Args
	oldSet := flag.CommandLine
	flag.CommandLine = flag.NewFlagSet(args[0], flag.ContinueOnError)
	os.Args = args
	t.Cleanup(func() {
		os.Args = oldArgs
		flag.CommandLine = oldSet
	})
}

func runMainWithArgs(t *testing.T, args ...string) {
	t.Helper()
	flag.CommandLine = flag.NewFlagSet(args[0], flag.ContinueOnError)
	os.Args = args
	main()
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

func TestReadFileAndWriteFile(t *testing.T) {
	root := withTempCWD(t)
	file := filepath.Join(root, "data.txt")

	if err := WriteFile(file, "hello"); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	got, err := ReadFile(file)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if string(got) != "hello" {
		t.Fatalf("unexpected content: %q", string(got))
	}

	_, err = ReadFile(filepath.Join(root, "missing.txt"))
	if err == nil {
		t.Fatalf("expected missing file error")
	}
}

func TestMainSuccessLinuxAndFavicon(t *testing.T) {
	root := withTempCWD(t)
	resetFlags(t, "pre-build")

	writeFile(t, filepath.Join(root, "build", "base-daemon.json"), []byte(`{"name":"demo"}`))
	wantIcon := []byte("ICON")
	writeFile(t, filepath.Join(root, "build", "base-favicon.ico"), wantIcon)

	runMainWithArgs(t, "pre-build", "-os", "linux", "-arch", "arm64", "-tag", "1.2.3")

	daemonData, err := os.ReadFile(filepath.Join(root, "daemon.json"))
	if err != nil {
		t.Fatalf("read daemon.json failed: %v", err)
	}
	var cfg JsonDaemonConfig
	if err := json.Unmarshal(daemonData, &cfg); err != nil {
		t.Fatalf("unmarshal daemon.json failed: %v", err)
	}
	if cfg.Version != "1.2.3" || cfg.OS != "linux" || cfg.Arch != "arm64" || cfg.Path != "./aiOpenCAD" {
		t.Fatalf("unexpected daemon config: %+v", cfg)
	}

	icon, err := os.ReadFile(filepath.Join(root, "favicon.ico"))
	if err != nil {
		t.Fatalf("read favicon.ico failed: %v", err)
	}
	if string(icon) != string(wantIcon) {
		t.Fatalf("unexpected favicon content: %q", string(icon))
	}
	releaseConfigPath := filepath.Join(root, "build", "release.config.json")
	releaseConfigData, err := os.ReadFile(releaseConfigPath)
	if err != nil {
		t.Fatalf("release.config.json should be written: %v", err)
	}
	var releaseConfig map[string]any
	if err := json.Unmarshal(releaseConfigData, &releaseConfig); err != nil {
		t.Fatalf("release.config.json should contain json: %v", err)
	}
}

func TestMainSuccessWindowsPath(t *testing.T) {
	root := withTempCWD(t)
	resetFlags(t, "pre-build")

	writeFile(t, filepath.Join(root, "build", "base-daemon.json"), []byte(`{"name":"demo"}`))
	writeFile(t, filepath.Join(root, "build", "base-favicon.ico"), []byte("ICON"))

	runMainWithArgs(t, "pre-build", "-os", "windows", "-arch", "amd64", "-tag", "2.0.0")

	daemonData, err := os.ReadFile(filepath.Join(root, "daemon.json"))
	if err != nil {
		t.Fatalf("read daemon.json failed: %v", err)
	}
	var cfg JsonDaemonConfig
	if err := json.Unmarshal(daemonData, &cfg); err != nil {
		t.Fatalf("unmarshal daemon.json failed: %v", err)
	}
	if cfg.Path != "./aiOpenCAD.exe" {
		t.Fatalf("unexpected windows path: %q", cfg.Path)
	}
}

func TestMainHandlesReadAndJSONErrors(t *testing.T) {
	root := withTempCWD(t)
	resetFlags(t, "pre-build")

	runMainWithArgs(t, "pre-build")
	if _, err := os.Stat(filepath.Join(root, "daemon.json")); !os.IsNotExist(err) {
		t.Fatalf("daemon.json should not exist on missing config")
	}

	writeFile(t, filepath.Join(root, "build", "base-daemon.json"), []byte("{bad json"))
	runMainWithArgs(t, "pre-build")
	if _, err := os.Stat(filepath.Join(root, "daemon.json")); !os.IsNotExist(err) {
		t.Fatalf("daemon.json should not exist on invalid config")
	}
}

func TestMainHandlesWriteErrors(t *testing.T) {
	root := withTempCWD(t)
	resetFlags(t, "pre-build")

	writeFile(t, filepath.Join(root, "build", "base-daemon.json"), []byte(`{"name":"demo"}`))
	writeFile(t, filepath.Join(root, "build", "base-favicon.ico"), []byte("ICON"))

	if err := os.Mkdir(filepath.Join(root, "daemon.json"), 0o755); err != nil {
		t.Fatalf("mkdir daemon.json failed: %v", err)
	}
	runMainWithArgs(t, "pre-build")
	if _, err := os.Stat(filepath.Join(root, "favicon.ico")); err != nil {
		t.Fatalf("favicon.ico should still be written when daemon write fails: %v", err)
	}

	if err := os.RemoveAll(filepath.Join(root, "daemon.json")); err != nil {
		t.Fatalf("cleanup daemon.json dir failed: %v", err)
	}
	if err := os.Remove(filepath.Join(root, "favicon.ico")); err != nil {
		t.Fatalf("remove favicon.ico file failed: %v", err)
	}
	if err := os.Mkdir(filepath.Join(root, "favicon.ico"), 0o755); err != nil {
		t.Fatalf("mkdir favicon.ico failed: %v", err)
	}
	runMainWithArgs(t, "pre-build")
}

func TestMainMissingFaviconStopsCopy(t *testing.T) {
	root := withTempCWD(t)
	resetFlags(t, "pre-build")

	writeFile(t, filepath.Join(root, "build", "base-daemon.json"), []byte(`{"name":"demo"}`))
	runMainWithArgs(t, "pre-build")

	if _, err := os.Stat(filepath.Join(root, "daemon.json")); err != nil {
		t.Fatalf("daemon.json should exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "favicon.ico")); !os.IsNotExist(err) {
		t.Fatalf("favicon.ico should not exist when source icon missing")
	}
}
