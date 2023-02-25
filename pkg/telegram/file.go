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

type FileClient interface {
	GetFiles(ctx context.Context, ID int64, opts ...GetFileOption) (<-chan FileInfo, <-chan error)
	GetFilesFromNewMessages(ctx context.Context, ID int64) (<-chan FileInfo, <-chan error)
	Download(ctx context.Context, file FileInfo, out io.Writer) error
}

var _ FileClient = (*client)(nil)

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

var ErrNoFiles = errors.New("no files in message")

func (c *client) getFileInfoFromElem(elem messages.Elem) (FileInfo, error) {
	var from *tg.User
	peer, ok := elem.Msg.GetFromID()
	if ok {
		switch p := peer.(type) {
		case *tg.PeerUser:
			from = elem.Entities.Users()[p.UserID]

		default:
			return FileInfo{}, errors.Errorf("unsupported peer type %T", peer)
		}
	}

	file, ok := elem.File()
	if !ok {
		return FileInfo{}, ErrNoFiles
	}

	var size int64
	if doc, ok := elem.Document(); ok {
		size = doc.Size
	}

	return FileInfo{
		file: file,
		from: from,
		size: size,
	}, nil
}

// GetFiles returns channel for file IDs and error channel.
func (c *client) GetFiles(ctx context.Context, ID int64, opts ...GetFileOption) (<-chan FileInfo, <-chan error) {
	fileChan := make(chan FileInfo)
	errChan := make(chan error)

	go func() {
		c.logger.Info("getting files from chat", zap.Int64("chat_id", ID))

		defer close(fileChan)
		defer close(errChan)

		options := getfileOptions{
			limit: int(^uint(0) >> 1), // MaxInt
		}
		for _, opt := range opts {
			if err := opt.apply(&options); err != nil {
				errChan <- err
				return
			}
		}

		var fileCounter int64

		inputPeer, err := c.getInputPeer(ctx, ID)
		if err != nil {
			errChan <- err
			return
		}

		queryBuilder := query.Messages(c.client.API()).GetHistory(inputPeer)
		queryBuilder = queryBuilder.OffsetDate(options.offsetDate)
		queryBuilder = queryBuilder.BatchSize(100)

		if err := queryBuilder.ForEach(ctx, func(ctx context.Context, elem messages.Elem) error {
			if atomic.LoadInt64(&fileCounter) >= int64(options.limit) {
				return fmt.Errorf("limit reached: %d", options.limit)
			}

			file, err := c.getFileInfoFromElem(elem)
			if err != nil {
				if !errors.Is(err, ErrNoFiles) {
					errChan <- err
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
			errChan <- err
			return
		}
	}()

	return fileChan, errChan
}

func (c *client) GetFilesFromNewMessages(ctx context.Context, ID int64) (<-chan FileInfo, <-chan error) {
	fileChan := make(chan FileInfo)
	errChan := make(chan error)

	go func() {
		c.logger.Info("getting files from new messages", zap.Int64("chat_id", ID))

		var fileCounter int64
		c.dispatcher.OnNewChannelMessage(func(ctx context.Context, e tg.Entities, update *tg.UpdateNewChannelMessage) error {
			nonEmpty, ok := update.Message.AsNotEmpty()
			if !ok {
				errChan <- fmt.Errorf("empty message")
				return nil
			}

			var peerID int64
			switch peer := nonEmpty.GetPeerID().(type) {
			case *tg.PeerChat:
				peerID = peer.GetChatID()

			case *tg.PeerChannel:
				peerID = peer.GetChannelID()

			default:
				errChan <- fmt.Errorf("unsupported peer type %T", peer)
				return nil
			}

			if peerID != ID {
				return nil
			}

			switch msg := nonEmpty.(type) {
			case *tg.Message:
				c.logger.Debug("got message", zap.Int("id", msg.ID), zap.String("message", msg.Message))

			default:
				errChan <- fmt.Errorf("unsupported message type %T", msg)
				return nil
			}

			entities := peer.EntitiesFromUpdate(e)

			msgPeer, err := entities.ExtractPeer(nonEmpty.GetPeerID())
			if err != nil {
				msgPeer = &tg.InputPeerEmpty{}
			}

			file, err := c.getFileInfoFromElem(messages.Elem{
				Msg:      nonEmpty,
				Peer:     msgPeer,
				Entities: entities,
			})
			if err != nil {
				if !errors.Is(err, ErrNoFiles) {
					errChan <- err
					return nil
				}

				return nil
			}

			select {
			case fileChan <- file:
				atomic.AddInt64(&fileCounter, 1)

			case <-ctx.Done():
				return ctx.Err()
			}

			return nil
		})
	}()

	return fileChan, errChan
}

func (c *client) Download(ctx context.Context, file FileInfo, out io.Writer) error {
	builder := c.downloader.Download(c.client.API(), file.file.Location)

	_, err := builder.Stream(ctx, out)
	if err != nil {
		return err
	}

	return nil
}
