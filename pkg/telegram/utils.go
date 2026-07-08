package telegram

import (
	"fmt"
	"os"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/johnnyipcom/tgdownloader/pkg/config"
	"github.com/johnnyipcom/tgdownloader/pkg/key"

	"github.com/gotd/td/clock"
	tgclient "github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/query/messages"
	"github.com/gotd/td/tg"
	"go.uber.org/zap"
)

const (
	defaultNTPAttemptsPerHost = 2
	defaultNTPRetryDelay      = 300 * time.Millisecond
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

func getClock(cfg config.Config, log *zap.Logger) (clock.Clock, error) {
	if !cfg.IsSet("clock.ntp.host") {
		return clock.System, nil
	}

	ntpHost := strings.TrimSpace(cfg.GetString("clock.ntp.host"))
	if ntpHost == "" {
		return clock.System, nil
	}

	hosts := buildNTPHosts(ntpHost)
	attemptsPerHost := cfg.GetInt("clock.ntp.attempts_per_host")
	if attemptsPerHost <= 0 {
		attemptsPerHost = defaultNTPAttemptsPerHost
	}

	var lastErr error
	for hostIdx, host := range hosts {
		for attempt := 1; attempt <= attemptsPerHost; attempt++ {
			ntpClock, err := NewNTPClock(host)
			if err == nil {
				if log != nil && (hostIdx > 0 || attempt > 1) {
					log.Warn("NTP clock initialized after retries/failover",
						zap.String("ntp_host", host),
						zap.Int("attempt", attempt),
						zap.Int("attempts_per_host", attemptsPerHost),
						zap.Int("host_index", hostIdx+1),
						zap.Int("hosts_total", len(hosts)),
					)
				}

				return ntpClock, nil
			}

			lastErr = err
			if log != nil {
				log.Warn("NTP query attempt failed",
					zap.String("ntp_host", host),
					zap.Int("attempt", attempt),
					zap.Int("attempts_per_host", attemptsPerHost),
					zap.Int("host_index", hostIdx+1),
					zap.Int("hosts_total", len(hosts)),
					zap.Error(err),
				)
			}

			if attempt < attemptsPerHost {
				time.Sleep(defaultNTPRetryDelay)
			}
		}
	}

	if log != nil {
		log.Warn("failed to initialize NTP clock after retries, falling back to system clock",
			zap.Strings("ntp_hosts", hosts),
			zap.Int("attempts_per_host", attemptsPerHost),
			zap.Error(lastErr),
		)
	}

	return clock.System, nil
}

func buildNTPHosts(primary string) []string {
	ordered := []string{primary}
	if primary == "pool.ntp.org" {
		ordered = append(ordered,
			"0.pool.ntp.org",
			"1.pool.ntp.org",
			"2.pool.ntp.org",
			"3.pool.ntp.org",
		)
	}

	result := make([]string, 0, len(ordered))
	seen := map[string]struct{}{}
	for _, host := range ordered {
		host = strings.TrimSpace(host)
		if host == "" {
			continue
		}
		if _, ok := seen[host]; ok {
			continue
		}
		seen[host] = struct{}{}
		result = append(result, host)
	}

	return result
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

func getPhotoFile(photoClass tg.PhotoClass) (*File, error) {
	photo, ok := photoClass.(*tg.Photo)
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

func getDocumentFile(docClass tg.DocumentClass) (*File, error) {
	doc, ok := docClass.(*tg.Document)
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

	if isSkippableTelegramDocumentName(name) {
		return nil, errNoFilesInMessage
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

func getFilesFromMessageMedia(media tg.MessageMediaClass) ([]*File, error) {
	switch m := media.(type) {
	case *tg.MessageMediaPhoto:
		photo, ok := m.GetPhoto()
		if !ok {
			return nil, errNoFilesInMessage
		}

		file, err := getPhotoFile(photo)
		if err != nil {
			return nil, err
		}

		return []*File{file}, nil
	case *tg.MessageMediaDocument:
		doc, ok := m.GetDocument()
		if !ok {
			return nil, errNoFilesInMessage
		}

		file, err := getDocumentFile(doc)
		if err != nil {
			return nil, err
		}

		return []*File{file}, nil
	case *tg.MessageMediaPaidMedia:
		files := make([]*File, 0, len(m.ExtendedMedia))
		hasLockedPreview := false
		for _, extended := range m.ExtendedMedia {
			switch e := extended.(type) {
			case *tg.MessageExtendedMedia:
				nestedFiles, err := getFilesFromMessageMedia(e.Media)
				if err != nil {
					if err == errNoFilesInMessage {
						continue
					}

					return nil, err
				}

				files = append(files, nestedFiles...)
			case *tg.MessageExtendedMediaPreview:
				hasLockedPreview = true
			}
		}

		if len(files) > 0 {
			return files, nil
		}

		if hasLockedPreview {
			return nil, errPaidMediaLocked
		}

		return nil, errNoFilesInMessage
	case *tg.MessageMediaInvoice:
		extended, ok := m.GetExtendedMedia()
		if !ok {
			return nil, errNoFilesInMessage
		}

		switch e := extended.(type) {
		case *tg.MessageExtendedMedia:
			return getFilesFromMessageMedia(e.Media)
		case *tg.MessageExtendedMediaPreview:
			return nil, errPaidMediaLocked
		default:
			return nil, errNoFilesInMessage
		}
	default:
		return nil, errNoFilesInMessage
	}
}

func getFileFromMessageMedia(media tg.MessageMediaClass) (*File, error) {
	files, err := getFilesFromMessageMedia(media)
	if err != nil {
		return nil, err
	}

	if len(files) == 0 {
		return nil, errNoFilesInMessage
	}

	return files[0], nil
}

func isSkippableTelegramDocumentName(name string) bool {
	normalized := strings.TrimSpace(strings.ReplaceAll(name, "\\", "/"))
	if normalized == "" {
		return false
	}

	base := strings.ToLower(path.Base(normalized))
	return base == "thumbs.db" || base == ".ds_store" || base == "desktop.ini"
}

func getFilesFromMessageElem(elem messages.Elem) ([]*File, error) {
	msg, ok := elem.Msg.(*tg.Message)
	if !ok {
		return nil, errNoFilesInMessage
	}

	return getFilesFromMessageMedia(msg.Media)
}
