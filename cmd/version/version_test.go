package version

import "testing"

func TestVersion(t *testing.T) {
	if got := Version(); got != "v0.7.0" {
		t.Fatalf("Version() = %q, want %q", got, "v0.7.0")
	}
}
