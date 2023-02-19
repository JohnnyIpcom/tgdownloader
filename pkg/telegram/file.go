package telegram

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync/atomic"

	"github.com/gotd/td/telegram/query"
	"github.com/gotd/td/telegram/query/messages"
	"github.com/gotd/td/tg"
	"go.uber.org/zap"
)

type FileInfo struct {
	file   messages.File
	fromID int64
	size   int64
}

func (f FileInfo) Size() int64 {
	return f.size
}

func (f FileInfo) String() string {
	return fmt.Sprintf("%v %d %d", f.file, f.fromID, f.size)
}

func (f FileInfo) FromID() int64 {
	return f.fromID
}

func (f FileInfo) Filename() string {
	return f.file.Name
}

type FileClient interface {
	GetFiles(ctx context.Context, chat ChatInfo, opts ...GetFileOption) (<-chan FileInfo, <-chan error)
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

// GetFiles returns channel for file IDs and error channel.
func (c *client) GetFiles(ctx context.Context, chat ChatInfo, opts ...GetFileOption) (<-chan FileInfo, <-chan error) {
	fileChan := make(chan FileInfo)
	errChan := make(chan error)

	go func() {
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

		var inputPeer tg.InputPeerClass
		switch chat.Type {
		case ChatTypeChat:
			chat, err := c.peerMgr.GetChat(ctx, chat.ID)
			if err != nil {
				errChan <- err
				return
			}

			inputPeer = chat.InputPeer()

		case ChatTypeChannel:
			channel, err := c.peerMgr.GetChannel(ctx, &tg.InputChannel{ChannelID: chat.ID})
			if err != nil {
				errChan <- err
				return
			}

			inputPeer = channel.InputPeer()

		default:
			errChan <- errors.New("unsupported chat type")
			return
		}

		queryBuilder := query.Messages(c.client.API()).GetHistory(inputPeer)
		queryBuilder = queryBuilder.OffsetDate(options.offsetDate)
		queryBuilder = queryBuilder.BatchSize(100)

		if err := queryBuilder.ForEach(ctx, func(ctx context.Context, elem messages.Elem) error {
			if atomic.LoadInt64(&fileCounter) >= int64(options.limit) {
				return fmt.Errorf("limit reached: %d", options.limit)
			}

			var fromID int64
			peer, ok := elem.Msg.GetFromID()
			if ok {
				switch peer := peer.(type) {
				case *tg.PeerUser:
					fromID = peer.GetUserID()

				default:
					return nil
				}
			}

			switch msg := elem.Msg.(type) {
			case *tg.Message:
				c.logger.Debug("got message", zap.Int64("from_id", fromID), zap.String("msg", msg.Message))

			default:
				return nil
			}

			if options.userID != 0 && fromID != options.userID {
				return nil
			}

			file, ok := elem.File()
			if !ok {
				return nil
			}

			fileInfo := FileInfo{
				file:   file,
				fromID: fromID,
			}

			if doc, ok := elem.Document(); ok {
				fileInfo.size = doc.Size
			}

			select {
			case fileChan <- fileInfo:
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

func (c *client) Download(ctx context.Context, file FileInfo, out io.Writer) error {
	builder := c.downloader.Download(c.client.API(), file.file.Location)

	_, err := builder.Stream(ctx, out)
	if err != nil {
		return err
	}

	return nil
}
