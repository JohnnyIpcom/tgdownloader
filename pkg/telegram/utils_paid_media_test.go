package telegram

import (
	"errors"
	"testing"

	"github.com/gotd/td/tg"
)

func TestGetFileFromMessageMediaPaidMediaPurchased(t *testing.T) {
	t.Parallel()

	media := &tg.MessageMediaPaidMedia{
		StarsAmount: 10,
		ExtendedMedia: []tg.MessageExtendedMediaClass{
			&tg.MessageExtendedMedia{
				Media: func() tg.MessageMediaClass {
					docMedia := &tg.MessageMediaDocument{}
					docMedia.SetDocument(&tg.Document{
						ID:         42,
						AccessHash: 100,
						Date:       1,
						MimeType:   "image/jpeg",
						Size:       77,
						DCID:       2,
						Attributes: []tg.DocumentAttributeClass{
							&tg.DocumentAttributeFilename{FileName: "paid.jpg"},
						},
					})

					return docMedia
				}(),
			},
		},
	}

	file, err := getFileFromMessageMedia(media)
	if err != nil {
		t.Fatalf("getFileFromMessageMedia() error = %v", err)
	}

	if file == nil {
		t.Fatal("getFileFromMessageMedia() returned nil file")
	}

	if got, want := file.Name(), "paid.jpg"; got != want {
		t.Fatalf("file.Name() = %q, want %q", got, want)
	}
}

func TestGetFileFromMessageMediaPaidMediaPreviewLocked(t *testing.T) {
	t.Parallel()

	media := &tg.MessageMediaPaidMedia{
		StarsAmount: 5,
		ExtendedMedia: []tg.MessageExtendedMediaClass{
			&tg.MessageExtendedMediaPreview{},
		},
	}

	_, err := getFileFromMessageMedia(media)
	if !errors.Is(err, errPaidMediaLocked) {
		t.Fatalf("getFileFromMessageMedia() error = %v, want %v", err, errPaidMediaLocked)
	}
}

func TestGetFileFromMessageMediaInvoiceExtendedPaid(t *testing.T) {
	t.Parallel()

	media := &tg.MessageMediaInvoice{}
	media.SetExtendedMedia(&tg.MessageExtendedMedia{
		Media: func() tg.MessageMediaClass {
			photoMedia := &tg.MessageMediaPhoto{}
			photoMedia.SetPhoto(&tg.Photo{
				ID:         777,
				AccessHash: 200,
				Date:       2,
				DCID:       4,
				Sizes: []tg.PhotoSizeClass{
					&tg.PhotoSize{Type: "x", Size: 123},
				},
			})

			return photoMedia
		}(),
	})

	file, err := getFileFromMessageMedia(media)
	if err != nil {
		t.Fatalf("getFileFromMessageMedia() error = %v", err)
	}

	if file == nil {
		t.Fatal("getFileFromMessageMedia() returned nil file")
	}

	if file.Size() != 123 {
		t.Fatalf("file.Size() = %d, want 123", file.Size())
	}
}

func TestGetFilesFromMessageMediaPaidMediaMultiple(t *testing.T) {
	t.Parallel()

	media := &tg.MessageMediaPaidMedia{
		StarsAmount: 15,
		ExtendedMedia: []tg.MessageExtendedMediaClass{
			&tg.MessageExtendedMedia{
				Media: func() tg.MessageMediaClass {
					docMedia := &tg.MessageMediaDocument{}
					docMedia.SetDocument(&tg.Document{
						ID:         1001,
						AccessHash: 11,
						Date:       1,
						MimeType:   "image/jpeg",
						Size:       10,
						DCID:       2,
						Attributes: []tg.DocumentAttributeClass{
							&tg.DocumentAttributeFilename{FileName: "first.jpg"},
						},
					})

					return docMedia
				}(),
			},
			&tg.MessageExtendedMedia{
				Media: func() tg.MessageMediaClass {
					docMedia := &tg.MessageMediaDocument{}
					docMedia.SetDocument(&tg.Document{
						ID:         1002,
						AccessHash: 12,
						Date:       1,
						MimeType:   "image/jpeg",
						Size:       20,
						DCID:       2,
						Attributes: []tg.DocumentAttributeClass{
							&tg.DocumentAttributeFilename{FileName: "second.jpg"},
						},
					})

					return docMedia
				}(),
			},
		},
	}

	files, err := getFilesFromMessageMedia(media)
	if err != nil {
		t.Fatalf("getFilesFromMessageMedia() error = %v", err)
	}

	if got, want := len(files), 2; got != want {
		t.Fatalf("len(files) = %d, want %d", got, want)
	}

	if files[0].Name() != "first.jpg" || files[1].Name() != "second.jpg" {
		t.Fatalf("unexpected file names: %q, %q", files[0].Name(), files[1].Name())
	}
}
