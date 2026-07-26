package viper

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadWritesStatusToInjectedWriter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tgdownloader.yaml")
	if err := os.WriteFile(path, []byte("telegram:\n  app:\n    id: 1\n"), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	config := NewConfig()
	var output bytes.Buffer

	if err := config.Load("tgdownloader", path, &output); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := output.String(); !strings.Contains(got, "Using config file: "+path) {
		t.Fatalf("status output = %q", got)
	}
}
