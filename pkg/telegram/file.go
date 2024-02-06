package telegram

import (
	"context"
	"fmt"
	"io"
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
	file messages.File
	peer peers.Peer
	size int64

	hashtags []string
}

func (f File) Size() int64 {
	return f.size
}

func (f File) String() string {
	return fmt.Sprintf("%v %d %d", f.file, f.PeerID(), f.size)
}

func (f File) PeerID() int64 {
	if f.peer == nil {
		return 0
	}

	return f.peer.ID()
}

func (f File) Username() (string, bool) {
	if f.peer == nil {
		return "", false
	}

	return f.peer.Username()
}

func (f File) Filename() string {
	return f.file.Name
}

func (f File) Hashtags() []string {
	return f.hashtags
}

type FileService interface {
	GetFiles(ctx context.Context, peer peers.Peer, opts ...GetFileOption) (<-chan File, error)
	GetFilesFromNewMessages(ctx context.Context, ID int64) (<-chan File, error)
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
				return ErrorLimitReached
			}

			file, err := s.client.GetFileFromElem(ctx, elem)
			if err != nil {
				if !errors.Is(err, errNoFilesInMessage) {
					s.logger.Error("failed to get file info from elem", zap.Error(err))
					return nil
				}

				return nil
			}

			if options.userID != 0 && file.PeerID() != options.userID {
				return nil
			}

			msgContent, ok := elem.Msg.(*tg.Message)
			if ok {
				file.hashtags = findHashtags(msgContent.GetMessage())
			}

			select {
			case fileChan <- file:
				atomic.AddInt64(&fileCounter, 1)

			case <-ctx.Done():
				return ctx.Err()
			}

			return nil
		}); err != nil {
			if !errors.Is(err, ErrorLimitReached) {
				s.logger.Error("failed to get files", zap.Error(err))
				return
			}

			return
		}
	}()

	return fileChan, nil
}

// GetFilesFromNewChannelMessages returns files from new messages.
func (s *fileService) GetFilesFromNewMessages(ctx context.Context, ID int64) (<-chan File, error) {
	fileChan := make(chan File)

	onNewMessage := func(ctx context.Context, e tg.Entities, msg tg.MessageClass) error {
		nonEmpty, ok := msg.AsNotEmpty()
		if !ok {
			s.logger.Debug("empty message")
			return nil
		}

		var peerID int64
		switch peer := nonEmpty.GetPeerID().(type) {
		case *tg.PeerChat:
			peerID = peer.GetChatID()

		case *tg.PeerChannel:
			peerID = peer.GetChannelID()

		case *tg.PeerUser:
			peerID = peer.GetUserID()

		default:
			s.logger.Debug("unsupported peer type", zap.Any("peer", peer))
			return nil
		}

		if peerID != ID {
			return nil
		}

		entities := peer.EntitiesFromUpdate(e)

		msgPeer, err := entities.ExtractPeer(nonEmpty.GetPeerID())
		if err != nil {
			msgPeer = &tg.InputPeerEmpty{}
		}

		file, err := s.client.GetFileFromElem(ctx, messages.Elem{
			Msg:      nonEmpty,
			Peer:     msgPeer,
			Entities: entities,
		})
		if err != nil {
			if !errors.Is(err, errNoFilesInMessage) {
				s.logger.Error("failed to get file info from elem", zap.Error(err))
				return nil
			}

			return nil
		}

		msgContent, ok := nonEmpty.(*tg.Message)
		if ok {
			file.hashtags = findHashtags(msgContent.GetMessage())
		}

		select {
		case fileChan <- file:
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
	builder := downloader.NewDownloader().Download(s.client.API(), file.file.Location)
	_, err := builder.Stream(ctx, out)
	if err != nil {
		return err
	}

	return nil
}
