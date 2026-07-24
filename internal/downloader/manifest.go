package downloader

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/spf13/afero"
)

const (
	fileManifestName    = ".tgdownloader-files.json"
	fileManifestVersion = 1
)

type fileManifest struct {
	Version int                          `json:"version"`
	Paths   map[string]map[string]string `json:"paths"`
}

func newFileManifest() fileManifest {
	return fileManifest{
		Version: fileManifestVersion,
		Paths:   make(map[string]map[string]string),
	}
}

func loadFileManifest(fs afero.Fs, filename string) (fileManifest, error) {
	manifest := newFileManifest()
	data, err := afero.ReadFile(fs, filename)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return manifest, nil
		}
		return manifest, fmt.Errorf("read file manifest: %w", err)
	}

	if err := json.Unmarshal(data, &manifest); err != nil {
		return newFileManifest(), fmt.Errorf("decode file manifest: %w", err)
	}
	if manifest.Version != fileManifestVersion {
		return newFileManifest(), fmt.Errorf("unsupported file manifest version %d", manifest.Version)
	}
	if manifest.Paths == nil {
		manifest.Paths = make(map[string]map[string]string)
	}

	return manifest, nil
}

func saveFileManifest(fs afero.Fs, filename string, manifest fileManifest) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("encode file manifest: %w", err)
	}
	data = append(data, '\n')

	temporary := filename + ".tmp"
	if err := afero.WriteFile(fs, temporary, data, 0600); err != nil {
		return fmt.Errorf("write temporary file manifest: %w", err)
	}
	if err := fs.Remove(filename); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("replace file manifest: %w", err)
	}
	if err := fs.Rename(temporary, filename); err != nil {
		return fmt.Errorf("rename file manifest: %w", err)
	}
	return nil
}

func (m fileManifest) lookup(logicalPath, identity string) (string, bool) {
	identities, ok := m.Paths[logicalPath]
	if !ok {
		return "", false
	}
	actualPath, ok := identities[identity]
	return actualPath, ok
}

func (m fileManifest) assign(logicalPath, identity, actualPath string) {
	identities, ok := m.Paths[logicalPath]
	if !ok {
		identities = make(map[string]string)
		m.Paths[logicalPath] = identities
	}
	identities[identity] = actualPath
}

func cleanManifestPath(filename string) (string, bool) {
	cleaned := path.Clean(filename)
	if cleaned == "." || cleaned == ".." || path.IsAbs(cleaned) || strings.HasPrefix(cleaned, "../") {
		return "", false
	}
	return cleaned, true
}
