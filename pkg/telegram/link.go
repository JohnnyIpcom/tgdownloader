package telegram

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/gotd/td/telegram/peers"
	"github.com/gotd/td/telegram/query"
	"github.com/gotd/td/telegram/query/messages"
	"github.com/gotd/td/tg"
	"github.com/johnnyipcom/tgdownloader/pkg/apperr"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type ExternalLink struct {
	URL       string
	Message   string
	MessageID int
	Metadata  map[string]interface{}
}

type LinkService interface {
	GetYandexDiskLinks(ctx context.Context, peer peers.Peer, opts ...GetAllFilesOption) (<-chan ExternalLink, error)
}

type linkService service

var _ LinkService = (*linkService)(nil)

var yandexDiskLinkPattern = regexp.MustCompile(`https?://(?:yadi\.sk|disk\.yandex\.[^/\s]+)/[^\s]+`)

func extractYandexDiskLinks(message string) []string {
	matches := yandexDiskLinkPattern.FindAllString(message, -1)
	if len(matches) == 0 {
		return nil
	}

	cleaned := make([]string, 0, len(matches))
	seen := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		link := strings.TrimRight(match, ".,;:!?)]}>'\"")
		if link == "" {
			continue
		}
		if _, ok := seen[link]; ok {
			continue
		}
		seen[link] = struct{}{}
		cleaned = append(cleaned, link)
	}

	return cleaned
}

func (s *linkService) GetYandexDiskLinks(ctx context.Context, p peers.Peer, opts ...GetAllFilesOption) (<-chan ExternalLink, error) {
	options := getAllFilesOption{
		limit: int(^uint(0) >> 1), // MaxInt
	}
	for _, opt := range opts {
		if err := opt.apply(&options); err != nil {
			return nil, apperr.Wrap("telegram.link.get_yadisk_links.options", err)
		}
	}

	var linkCounter int64
	linkChan := make(chan ExternalLink)

	go func() {
		defer close(linkChan)

		queryBuilder := query.Messages(s.client.API()).GetHistory(p.InputPeer())
		queryBuilder = queryBuilder.OffsetDate(options.offsetDate)
		queryBuilder = queryBuilder.BatchSize(100)

		if err := queryBuilder.ForEach(ctx, func(ctx context.Context, elem messages.Elem) error {
			if atomic.LoadInt64(&linkCounter) >= int64(options.limit) {
				s.logger.Info("limit reached", zap.Int64("limit", int64(options.limit)))
				return errLimitReached
			}

			message, ok := elem.Msg.(*tg.Message)
			if !ok {
				return nil
			}

			links := extractYandexDiskLinks(message.GetMessage())
			if len(links) == 0 {
				return nil
			}

			metadata := map[string]interface{}{}
			visibleName := p.VisibleName()
			if visibleName != "" {
				metadata["peername"] = visibleName
			} else {
				metadata["peername"] = strconv.FormatInt(p.ID(), 10)
			}

			hashtags := extractHashtags(message.GetMessage())
			if len(hashtags) > 0 {
				metadata["hashtags"] = hashtags
			}

			for _, link := range links {
				if atomic.LoadInt64(&linkCounter) >= int64(options.limit) {
					return errLimitReached
				}

				external := ExternalLink{
					URL:       link,
					Message:   message.GetMessage(),
					MessageID: message.GetID(),
					Metadata:  metadata,
				}

				select {
				case linkChan <- external:
					atomic.AddInt64(&linkCounter, 1)
				case <-ctx.Done():
					return ctx.Err()
				}
			}

			return nil
		}); err != nil {
			if !errors.Is(err, errLimitReached) {
				s.logger.Error("failed to get yandex disk links", zap.Error(apperr.New("telegram.link.get_yadisk_links.iterate", apperr.KindNetwork, err)))
			}
		}
	}()

	return linkChan, nil
}
