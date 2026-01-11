package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadEnvFileSetsUnsetVars(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "env")
	content := "FOO=bar\nBAZ=qux\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write env file: %v", err)
	}
	os.Unsetenv("FOO")
	if err := LoadEnvFile(path); err != nil {
		t.Fatalf("LoadEnvFile err: %v", err)
	}
	if os.Getenv("FOO") != "bar" {
		t.Fatalf("expected FOO=bar, got %q", os.Getenv("FOO"))
	}
}

func TestLoadEnvFileDoesNotOverride(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "env")
	content := "FOO=bar\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write env file: %v", err)
	}
	os.Setenv("FOO", "existing")
	if err := LoadEnvFile(path); err != nil {
		t.Fatalf("LoadEnvFile err: %v", err)
	}
	if os.Getenv("FOO") != "existing" {
		t.Fatalf("expected existing to remain, got %q", os.Getenv("FOO"))
	}
}
