package telegram

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/gotd/td/fileid"
	"github.com/gotd/td/telegram/query/hasher"
	"github.com/gotd/td/tg"
	"go.uber.org/zap"
)

type File struct {
	fileID fileid.FileID
	size   int64
}

func (f File) ID() int64 {
	return f.fileID.ID
}

func (f File) Size() int64 {
	return f.size
}

func (f File) GetExtension() string {
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
	GetFiles(ctx context.Context, chat Chat) (<-chan File, <-chan error)
	Download(ctx context.Context, file File, out io.Writer) error
}

var _ FileClient = (*client)(nil)

// GetFiles returns channel for file IDs and error channel.
func (c *client) GetFiles(ctx context.Context, chat Chat) (<-chan File, <-chan error) {
	fileChan := make(chan File)
	errChan := make(chan error)

	hasher := hasher.Hasher{}

	go func() {
		defer close(fileChan)
		defer close(errChan)

		var offsetID int

		hasher.Reset()
		for {
			history, err := c.client.API().MessagesGetHistory(ctx, &tg.MessagesGetHistoryRequest{
				Peer: &tg.InputPeerChannel{
					ChannelID:  chat.ID,
					AccessHash: chat.AccessHash,
				},
				OffsetID:  offsetID,
				AddOffset: 0,
				Limit:     100,
				MaxID:     0,
				MinID:     0,
				Hash:      0,
			})
			if err != nil {
				errChan <- err
				return
			}

			messages, ok := history.AsModified()
			if !ok {
				errChan <- fmt.Errorf("unexpected response type: %T", history)
				return
			}

			for _, message := range messages.GetMessages() {
				switch message := message.(type) {
				case *tg.Message:
					if message.Media != nil {
						switch media := message.Media.(type) {
						case *tg.MessageMediaPhoto:
							photo, ok := media.GetPhoto()
							if !ok {
								continue
							}

							p, ok := photo.AsNotEmpty()
							if !ok {
								continue
							}

							file := File{
								fileID: fileid.FromPhoto(p, 'x'),
								size:   0, // TODO: get size
							}

							select {
							case <-ctx.Done():
								return

							case fileChan <- file:
								hasher.Update64(uint64(p.GetID()))
							}

						case *tg.MessageMediaDocument:
							document, ok := media.GetDocument()
							if !ok {
								continue
							}

							d, ok := document.AsNotEmpty()
							if !ok {
								continue
							}

							file := File{
								fileID: fileid.FromDocument(d),
								size:   d.GetSize(),
							}

							select {
							case <-ctx.Done():
								return

							case fileChan <- file:
								hasher.Update64(uint64(d.GetID()))
							}

						default:
							c.logger.Warn("unsupported media type", zap.Any("media", media))
						}
					}

					offsetID = message.GetID()

				case *tg.MessageService:
					offsetID = message.GetID()

				default:
					c.logger.Warn("unknown message type", zap.Any("message", message))
				}
			}
		}
	}()

	return fileChan, errChan
}

func (c *client) Download(ctx context.Context, file File, out io.Writer) error {
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
