package version

import (
	_ "embed"
)

//go:embed version.txt
var version string

func removeBOM(s string) string {
	if len(s) > 0 && s[0] == 0xEF && s[1] == 0xBB && s[2] == 0xBF {
		return s[3:]
	}

	return s
}

// Version returns the version of the application.
func Version() string {
	return removeBOM(version)
}
