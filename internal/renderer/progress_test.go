package renderer

import "testing"

func TestBytesTrackerWriteWithoutWriter(t *testing.T) {
	t.Parallel()

	progress := NewProgress()
	tracker := progress.BytesTracker(nil, "test", 4)
	defer progress.Stop()

	n, err := tracker.Write([]byte("test"))
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if n != 4 {
		t.Fatalf("Write() = %d, want 4", n)
	}
	tracker.Done()
}
