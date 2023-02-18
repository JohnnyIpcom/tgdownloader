package telegram

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync/atomic"
	"time"

	"github.com/gotd/td/fileid"
	"github.com/gotd/td/telegram/query"
	"github.com/gotd/td/telegram/query/messages"
	"github.com/gotd/td/tg"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

type FileInfo struct {
	fileID fileid.FileID
	fromID int64
	date   time.Time
	size   int64
}

func (f FileInfo) ID() int64 {
	return f.fileID.ID
}

func (f FileInfo) Size() int64 {
	return f.size
}

func (f FileInfo) String() string {
	return fmt.Sprintf("%v %d %s %d", f.fileID, f.fromID, f.date, f.size)
}

func (f FileInfo) FromID() int64 {
	return f.fromID
}

func (f FileInfo) Filename() string {
	return fmt.Sprintf("%d-%s", f.fileID.ID, f.date.Format("2006-01-02 15-04-05"))
}

func (f FileInfo) Extension() string {
	switch f.fileID.Type {
	case fileid.Thumbnail, fileid.ProfilePhoto, fileid.Photo:
		return ".jpg"

	case fileid.Video, fileid.Animation, fileid.VideoNote:
		return ".mp4"

	case fileid.Audio:
		return ".mp3"

	case fileid.Voice:
		return ".ogg"

	case fileid.Sticker:
		return ".png"
	}

	return ".dat"
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

		err := queryBuilder.ForEach(ctx, func(ctx context.Context, elem messages.Elem) error {
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
				c.logger.Info("got message", zap.Int64("from_id", fromID), zap.String("msg", msg.Message))

			default:
				return nil
			}

			if options.userID != 0 && fromID != options.userID {
				return nil
			}

			g := errgroup.Group{}
			g.Go(func() error {
				doc, ok := elem.Document()
				if !ok {
					return nil
				}

				atomic.AddInt64(&fileCounter, 1)
				fileChan <- FileInfo{
					fileID: fileid.FromDocument(doc),
					fromID: fromID,
					date:   time.Unix(int64(elem.Msg.GetDate()), 0),
					size:   doc.Size,
				}

				return nil
			})

			g.Go(func() error {
				photo, ok := elem.Photo()
				if !ok {
					return nil
				}

				atomic.AddInt64(&fileCounter, 1)
				fileChan <- FileInfo{
					fileID: fileid.FromPhoto(photo, 'x'),
					fromID: fromID,
					date:   time.Unix(int64(elem.Msg.GetDate()), 0),
					size:   0, // TODO: get size
				}

				return nil
			})

			return g.Wait()
		})

		if err != nil {
			errChan <- err
			return
		}
	}()

	return fileChan, errChan
}

func (c *client) Download(ctx context.Context, file FileInfo, out io.Writer) error {
	c.logger.Info(
		"downloading file",
		zap.Int64("file_id", file.fileID.ID),
		zap.Int64("access_hash", file.fileID.AccessHash),
		zap.Int("dc_id", file.fileID.DC),
	)

	loc, ok := file.fileID.AsInputFileLocation()
	if !ok {
		c.logger.Error("failed to convert fileID to InputFileLocation", zap.Int64("file_id", file.fileID.ID))
		return errors.New("failed to convert fileID to InputFileLocation")
	}

	builder := c.downloader.Download(c.client.API(), loc)

	_, err := builder.Stream(ctx, out)
	if err != nil {
		c.logger.Error("failed to download file", zap.Int64("file_id", file.fileID.ID), zap.Error(err))
		return err
	}

	return nil
}
