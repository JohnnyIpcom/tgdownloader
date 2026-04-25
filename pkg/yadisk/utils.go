package yadisk

import (
	"path/filepath"
	"strings"
)

// IsVideoFile checks if a file is a video based on its extension.
func IsVideoFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(name)))
	switch ext {
	case ".mp4", ".mov", ".avi", ".mkv", ".wmv", ".flv", ".webm", ".m4v", ".mpg", ".mpeg", ".3gp":
		return true
	default:
		return false
	}
}

// IsSkippableYandexItem checks if an error indicates a file that should be skipped
// (e.g., thumbs.db, system files).
func IsSkippableYandexItem(name string, err error) bool {
	if err == nil {
		return false
	}

	errText := strings.ToLower(err.Error())
	if !strings.Contains(errText, "empty href") {
		return false
	}

	fileName := strings.ToLower(strings.TrimSpace(name))
	switch fileName {
	case "thumbs.db", ".ds_store", "desktop.ini":
		return true
	default:
		return false
	}
}

// BuildSubdirectories constructs subdirectories from metadata.
// saveByHashtags indicates whether to include hashtags as subdirectories.
func BuildSubdirectories(metadata map[string]interface{}, saveByHashtags bool) []string {
	if metadata == nil {
		return nil
	}

	var subdirs []string
	
	if peerName, ok := metadata["peername"].(string); ok && peerName != "" {
		subdirs = append(subdirs, peerName)
	}

	if saveByHashtags {
		if hashtags, ok := metadata["hashtags"].([]string); ok {
			subdirs = append(subdirs, hashtags...)
		}
	}

	return subdirs
}
