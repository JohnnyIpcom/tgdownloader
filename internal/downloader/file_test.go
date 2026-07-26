package downloader

import (
	"reflect"
	"testing"
)

func TestNewFileIgnoresAuthorDirectory(t *testing.T) {
	file := makeTelegramFile("photo.jpg")

	got := NewFile(file, WithSubdirs("Selected dialog"))

	want := []string{"Selected dialog"}
	if !reflect.DeepEqual(got.subdirs, want) {
		t.Fatalf("subdirectories = %q, want %q", got.subdirs, want)
	}
}

func TestNewFilePreservesHashtagDirectories(t *testing.T) {
	file := makeTelegramFile("photo.jpg")
	file.Metadata()["hashtags"] = []string{"travel", "photo"}

	got := NewFile(file, WithSubdirs("Selected dialog"), WithSaveByHashtags(true))

	want := map[string]bool{
		"Selected dialog": true,
		"travel":          true,
		"photo":           true,
	}
	if len(got.subdirs) != len(want) {
		t.Fatalf("subdirectories = %q, want %d entries", got.subdirs, len(want))
	}
	for _, subdir := range got.subdirs {
		if !want[subdir] {
			t.Fatalf("unexpected subdirectory %q in %q", subdir, got.subdirs)
		}
	}
}
