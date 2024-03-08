package telegram

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/johnnyipcom/tgdownloader/pkg/config"
	"github.com/johnnyipcom/tgdownloader/pkg/key"

	tgclient "github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/query/messages"
	"github.com/gotd/td/tg"
)

func extractHashtags(input string) []string {
	hashtags := []string{}
	regex := regexp.MustCompile(`#[^\s#]+`)

	matches := regex.FindAllString(input, -1)
	for _, match := range matches {
		trimmedTag := strings.TrimPrefix(match, "#")
		hashtags = append(hashtags, trimmedTag)
	}

	return hashtags
}

func getPublicKeys(cfg config.Config) ([]tgclient.PublicKey, error) {
	if !cfg.IsSet("mtproto.public_keys") {
		return nil, nil
	}

	var keys []tgclient.PublicKey

	publicKeys := cfg.GetStringSlice("mtproto.public_keys")
	for _, publicKey := range publicKeys {
		publicKeyData, err := os.ReadFile(publicKey)
		if err != nil {
			return nil, err
		}

		key, err := key.ParsePublicKey(publicKeyData)
		if err != nil {
			return nil, err
		}

		keys = append(keys, tgclient.PublicKey{
			RSA: key,
		})
	}

	return keys, nil
}

func getPhotoSize(sizes []tg.PhotoSizeClass) (string, int, bool) {
	size := sizes[len(sizes)-1]
	switch s := size.(type) {
	case *tg.PhotoSize:
		return s.Type, s.Size, true
	case *tg.PhotoSizeProgressive:
		return s.Type, s.Sizes[len(s.Sizes)-1], true
	}

	return "", 0, false
}

const dateLayout = "2006-01-02_15-04-05"

func getPhotoFromMessage(elem messages.Elem) (*File, error) {
	photo, ok := elem.Photo()
	if !ok {
		return nil, errNoFilesInMessage
	}

	thumbSize, size, ok := getPhotoSize(photo.Sizes)
	if !ok {
		return nil, errNoFilesInMessage
	}

	name := fmt.Sprintf(
		"photo%d_%s.jpg",
		photo.GetID(), time.Unix(int64(photo.Date), 0).Format(dateLayout),
	)

	return &File{
		name: name,
		size: int64(size),
		dc:   photo.DCID,

		location: &tg.InputPhotoFileLocation{
			ID:            photo.ID,
			AccessHash:    photo.AccessHash,
			FileReference: photo.FileReference,
			ThumbSize:     thumbSize,
		},

		metadata: map[string]interface{}{
			"mime_type":  "image/jpeg",
			"thumb_size": thumbSize,
		},
	}, nil
}

func getDocumentFromMessage(elem messages.Elem) (*File, error) {
	doc, ok := elem.Document()
	if !ok {
		return nil, errNoFilesInMessage
	}

	var name, ext string
	for _, attr := range doc.Attributes {
		switch v := attr.(type) {
		case *tg.DocumentAttributeImageSize:
			switch doc.MimeType {
			case "image/png":
				ext = ".png"
			case "image/webp":
				ext = ".webp"
			case "image/tiff":
				ext = ".tif"
			default:
				ext = ".jpg"
			}
		case *tg.DocumentAttributeAnimated:
			ext = ".gif"
		case *tg.DocumentAttributeSticker:
			ext = ".webp"
		case *tg.DocumentAttributeVideo:
			switch doc.MimeType {
			case "video/mpeg":
				ext = ".mpeg"
			case "video/webm":
				ext = ".webm"
			case "video/ogg":
				ext = ".ogg"
			default:
				ext = ".mp4"
			}
		case *tg.DocumentAttributeAudio:
			switch doc.MimeType {
			case "audio/webm":
				ext = ".webm"
			case "audio/aac":
				ext = ".aac"
			case "audio/ogg":
				ext = ".ogg"
			default:
				ext = ".mp3"
			}
		case *tg.DocumentAttributeFilename:
			name = v.FileName
		}
	}

	if name == "" {
		name = fmt.Sprintf(
			"doc%d_%s%s", doc.GetID(),
			time.Unix(int64(doc.Date), 0).Format(dateLayout),
			ext,
		)
	}

	return &File{
		name: name,
		size: doc.Size,
		dc:   doc.DCID,

		location: &tg.InputDocumentFileLocation{
			ID:            doc.ID,
			AccessHash:    doc.AccessHash,
			FileReference: doc.FileReference,
		},

		metadata: map[string]interface{}{
			"mime_type": doc.MimeType,
		},
	}, nil
}

func getFileFromMessage(elem messages.Elem) (*File, error) {
	msg, ok := elem.Msg.(*tg.Message)
	if !ok {
		return nil, errNoFilesInMessage
	}

	switch msg.Media.(type) {
	case *tg.MessageMediaPhoto:
		return getPhotoFromMessage(elem)
	case *tg.MessageMediaDocument:
		return getDocumentFromMessage(elem)
	}

	return nil, errNoFilesInMessage
}
