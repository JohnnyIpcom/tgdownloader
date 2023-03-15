package telegram

import (
	"context"
	"fmt"
	"io"
	"sync/atomic"

	"github.com/gotd/td/telegram/message/peer"
	"github.com/gotd/td/telegram/query"
	"github.com/gotd/td/telegram/query/messages"
	"github.com/gotd/td/tg"
	"github.com/johnnyipcom/tgdownloader/pkg/ctxlogger"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type FileInfo struct {
	file messages.File
	from *tg.User
	size int64
}

func (f FileInfo) Size() int64 {
	return f.size
}

func (f FileInfo) String() string {
	return fmt.Sprintf("%v %d %d", f.file, f.FromID(), f.size)
}

func (f FileInfo) FromID() int64 {
	if f.from == nil {
		return 0
	}

	return f.from.GetID()
}

func (f FileInfo) Username() string {
	if f.from == nil {
		return ""
	}

	username, ok := f.from.GetUsername()
	if !ok {
		return ""
	}

	return username
}

func (f FileInfo) Filename() string {
	return f.file.Name
}

type FileService interface {
	GetFiles(ctx context.Context, peer PeerInfo, opts ...GetFileOption) (<-chan FileInfo, error)
	GetFilesFromNewMessages(ctx context.Context, ID int64) (<-chan FileInfo, error)
	Download(ctx context.Context, file FileInfo, out io.Writer) error
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
func (s *fileService) GetFiles(ctx context.Context, peer PeerInfo, opts ...GetFileOption) (<-chan FileInfo, error) {
	options := getfileOptions{
		limit: int(^uint(0) >> 1), // MaxInt
	}
	for _, opt := range opts {
		if err := opt.apply(&options); err != nil {
			return nil, err
		}
	}

	var fileCounter int64

	inputPeer, err := s.client.getInputPeer(ctx, peer.TDLibPeerID())
	if err != nil {
		return nil, err
	}

	fileChan := make(chan FileInfo)
	go func() {
		defer close(fileChan)

		logger := ctxlogger.FromContext(ctx)

		queryBuilder := query.Messages(s.client.API()).GetHistory(inputPeer)
		queryBuilder = queryBuilder.OffsetDate(options.offsetDate)
		queryBuilder = queryBuilder.BatchSize(100)

		if err := queryBuilder.ForEach(ctx, func(ctx context.Context, elem messages.Elem) error {
			if atomic.LoadInt64(&fileCounter) >= int64(options.limit) {
				logger.Info("limit reached", zap.Int64("limit", int64(options.limit)))
				return ErrorLimitReached
			}

			file, err := getFileInfoFromElem(elem)
			if err != nil {
				if !errors.Is(err, errNoFilesInMessage) {
					logger.Error("failed to get file info from elem", zap.Error(err))
					return nil
				}

				return nil
			}

			if options.userID != 0 && file.FromID() != options.userID {
				return nil
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
				logger.Error("failed to get files", zap.Error(err))
				return
			}

			return
		}
	}()

	return fileChan, nil
}

// GetFilesFromNewChannelMessages returns files from new messages.
func (s *fileService) GetFilesFromNewMessages(ctx context.Context, ID int64) (<-chan FileInfo, error) {
	fileChan := make(chan FileInfo)

	s.client.dispatcher.OnNewMessage(func(ctx context.Context, e tg.Entities, update *tg.UpdateNewMessage) error {
		logger := ctxlogger.FromContext(ctx)

		nonEmpty, ok := update.Message.AsNotEmpty()
		if !ok {
			logger.Debug("empty message")
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
			logger.Debug("unsupported peer type", zap.Any("peer", peer))
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

		file, err := getFileInfoFromElem(messages.Elem{
			Msg:      nonEmpty,
			Peer:     msgPeer,
			Entities: entities,
		})
		if err != nil {
			if !errors.Is(err, errNoFilesInMessage) {
				logger.Error("failed to get file info from elem", zap.Error(err))
				return nil
			}

			return nil
		}

		select {
		case fileChan <- file:
			// pass

		case <-ctx.Done():
			return ctx.Err()
		}

		return nil
	})

	return fileChan, nil
}

func (s *fileService) Download(ctx context.Context, file FileInfo, out io.Writer) error {
	builder := s.client.downloader.Download(s.client.API(), file.file.Location)

	_, err := builder.Stream(ctx, out)
	if err != nil {
		return err
	}

	return nil
}
