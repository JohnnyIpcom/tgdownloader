package telegram

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"sync/atomic"

	"github.com/gotd/td/telegram/downloader"
	"github.com/gotd/td/telegram/message/peer"
	"github.com/gotd/td/telegram/peers"
	"github.com/gotd/td/telegram/query"
	"github.com/gotd/td/telegram/query/messages"
	"github.com/gotd/td/tg"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type File struct {
	name     string
	size     int64
	dc       int
	location tg.InputFileLocationClass
	metadata map[string]interface{}
}

func (f File) String() string {
	return fmt.Sprintf("File{dc: %d, name: %s, size: %d}", f.dc, f.name, f.size)
}

func (f File) DC() int {
	return f.dc
}

func (f File) Name() string {
	return f.name
}

func (f File) Size() int64 {
	return f.size
}

func (f File) Metadata() map[string]interface{} {
	return f.metadata
}

type FileService interface {
	GetFiles(ctx context.Context, peer peers.Peer, opts ...GetFileOption) (<-chan File, error)
	GetFilesFromNewMessages(ctx context.Context, peer peers.Peer, opts ...GetFileOption) (<-chan File, error)
	Download(ctx context.Context, file File, out io.Writer) error
}

type fileService service

var _ FileService = (*fileService)(nil)

type getfileOptions struct {
	userID     int64
	limit      int
	offsetDate int
}

type GetFileOption interface {
	apply(*getfileOptions) error
}

type getfileUserIDOption struct {
	userID int64
}

func (o getfileUserIDOption) apply(opts *getfileOptions) error {
	opts.userID = o.userID
	return nil
}

func GetFileWithUserID(userID int64) GetFileOption {
	return getfileUserIDOption{userID: userID}
}

type getfileLimitOption struct {
	limit int
}

func (o getfileLimitOption) apply(opts *getfileOptions) error {
	opts.limit = o.limit
	return nil
}

func GetFileWithLimit(limit int) GetFileOption {
	return getfileLimitOption{limit: limit}
}

type getfileOffsetDateOption struct {
	offsetDate int
}

func (o getfileOffsetDateOption) apply(opts *getfileOptions) error {
	opts.offsetDate = o.offsetDate
	return nil
}

func GetFileWithOffsetDate(offsetDate int) GetFileOption {
	return getfileOffsetDateOption{offsetDate: offsetDate}
}

func (s *fileService) extractFileFromMessage(ctx context.Context, elem messages.Elem) (*File, int64, error) {
	file, err := getFileFromMessage(ctx, elem)
	if err != nil {
		if errors.Is(err, errNoFilesInMessage) {
			return nil, 0, nil
		}

		return nil, 0, err
	}

	var peer peers.Peer
	fromID, ok := elem.Msg.GetFromID()
	if ok {
		from, err := s.client.ExtractPeer(ctx, elem.Entities, fromID)
		if err != nil {
			return nil, 0, fmt.Errorf("extract fromID: %w", err)
		}

		peer = from
	}

	if peer == nil {
		p, err := s.client.ExtractPeer(ctx, elem.Entities, elem.Msg.GetPeerID())
		if err != nil {
			return nil, 0, fmt.Errorf("extract peer: %w", err)
		}

		peer = p
	}

	file.metadata["peername"] = strconv.FormatInt(peer.ID(), 10)

	visibleName := peer.VisibleName()
	if visibleName != "" {
		file.metadata["peername"] = visibleName
	}

	msg, ok := elem.Msg.(*tg.Message)
	if ok {
		hashtags := extractHashtags(msg.GetMessage())
		if len(hashtags) > 0 {
			file.metadata["hashtags"] = hashtags
		}
	}

	return file, peer.ID(), nil
}

// GetFiles returns channel for file IDs and error channel.
func (s *fileService) GetFiles(ctx context.Context, peer peers.Peer, opts ...GetFileOption) (<-chan File, error) {
	options := getfileOptions{
		limit: int(^uint(0) >> 1), // MaxInt
	}
	for _, opt := range opts {
		if err := opt.apply(&options); err != nil {
			return nil, err
		}
	}

	var fileCounter int64

	inputPeer, err := s.client.GetInputPeer(ctx, peer.TDLibPeerID())
	if err != nil {
		return nil, err
	}

	fileChan := make(chan File)
	go func() {
		defer close(fileChan)

		queryBuilder := query.Messages(s.client.API()).GetHistory(inputPeer)
		queryBuilder = queryBuilder.OffsetDate(options.offsetDate)
		queryBuilder = queryBuilder.BatchSize(100)

		if err := queryBuilder.ForEach(ctx, func(ctx context.Context, elem messages.Elem) error {
			if atomic.LoadInt64(&fileCounter) >= int64(options.limit) {
				s.logger.Info("limit reached", zap.Int64("limit", int64(options.limit)))
				return errLimitReached
			}

			file, peerID, err := s.extractFileFromMessage(ctx, elem)
			if err != nil {
				return err
			}

			if file == nil || (options.userID > 0 && peerID != options.userID) {
				return nil
			}

			select {
			case fileChan <- *file:
				atomic.AddInt64(&fileCounter, 1)

			case <-ctx.Done():
				return ctx.Err()
			}

			return nil
		}); err != nil {
			if !errors.Is(err, errLimitReached) {
				s.logger.Error("failed to get files", zap.Error(err))
				return
			}

			return
		}
	}()

	return fileChan, nil
}

// GetFilesFromNewChannelMessages returns files from new messages.
func (s *fileService) GetFilesFromNewMessages(ctx context.Context, p peers.Peer, opts ...GetFileOption) (<-chan File, error) {
	fileChan := make(chan File)

	onNewMessage := func(ctx context.Context, e tg.Entities, msg tg.MessageClass) error {
		nonEmpty, ok := msg.AsNotEmpty()
		if !ok {
			s.logger.Debug("empty message")
			return nil
		}

		entities := peer.EntitiesFromUpdate(e)

		msgPeer, err := entities.ExtractPeer(nonEmpty.GetPeerID())
		if err != nil {
			msgPeer = &tg.InputPeerEmpty{}
		}

		file, peerID, err := s.extractFileFromMessage(ctx, messages.Elem{
			Msg:      nonEmpty,
			Peer:     msgPeer,
			Entities: entities,
		})

		if err != nil {
			return err
		}

		if file == nil || peerID != p.ID() {
			return nil
		}

		select {
		case fileChan <- *file:
			// pass

		case <-ctx.Done():
			return ctx.Err()
		}

		return nil
	}

	s.client.dispatcher.OnNewMessage(func(ctx context.Context, e tg.Entities, update *tg.UpdateNewMessage) error {
		return onNewMessage(ctx, e, update.Message)
	})

	s.client.dispatcher.OnNewChannelMessage(func(ctx context.Context, e tg.Entities, update *tg.UpdateNewChannelMessage) error {
		return onNewMessage(ctx, e, update.Message)
	})

	return fileChan, nil
}

func (s *fileService) Download(ctx context.Context, file File, out io.Writer) error {
	builder := downloader.NewDownloader().Download(s.client.API(), file.location)
	_, err := builder.Stream(ctx, out)
	if err != nil {
		return err
	}

	return nil
}
