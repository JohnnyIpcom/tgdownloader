package cmd

import (
	"context"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"time"

	"github.com/gotd/td/fileid"
	"github.com/gotd/td/telegram/downloader"
	"github.com/gotd/td/telegram/query/hasher"
	"github.com/gotd/td/tg"
	"github.com/jedib0t/go-pretty/v6/progress"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) {
	return f(p)
}

func newDownloadCmd(ctx context.Context, r *Root) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "download",
		Short: "Download files from a chat, channel or user",
		Long:  `Download files from a chat, channel or user.`,
		Run: func(cmd *cobra.Command, args []string) {
			r.client.Run(ctx, func(ctx context.Context, c *tg.Client) error {
				id, err := cmd.Flags().GetInt64("id")
				if err != nil {
					r.log.Error("failed to get id", zap.Error(err))
					return err
				}

				output, err := cmd.Flags().GetString("output")
				if err != nil {
					r.log.Error("failed to get output", zap.Error(err))
					return err
				}

				limit, err := cmd.Flags().GetInt("limit")
				if err != nil {
					r.log.Error("failed to get limit", zap.Error(err))
					return err
				}

				chats, err := c.MessagesGetAllChats(ctx, []int64{})
				if err != nil {
					r.log.Error("failed to get chats", zap.Error(err))
					return err
				}

				var peer *tg.InputPeerChannel
				for _, chatClass := range chats.GetChats() {
					switch chat := chatClass.(type) {
					case *tg.Channel:
						if chat.GetID() == id {
							peer = chat.AsInputPeer()
							break
						}

					default:
						continue
					}
				}

				if peer == nil {
					r.log.Error("failed to find channel")
					return fmt.Errorf("failed to find channel")
				}

				r.log.Info("found channel", zap.Int64("id", peer.ChannelID), zap.Int64("access_hash", peer.AccessHash))

				documents := make(chan *tg.Document, 5)
				photos := make(chan *tg.Photo, 5)

				hasher := hasher.Hasher{}

				g, ctx := errgroup.WithContext(ctx)
				g.Go(func() error {
					defer close(documents)
					defer close(photos)

					hasher.Reset()
					for {
						history, err := c.MessagesGetHistory(ctx, &tg.MessagesGetHistoryRequest{
							Peer: &tg.InputPeerChannel{
								ChannelID:  peer.ChannelID,
								AccessHash: peer.AccessHash,
							},
							Limit: limit,
						})

						if err != nil {
							r.log.Error("failed to get history", zap.Error(err))
							return err
						}

						messages, ok := history.AsModified()
						if !ok {
							r.log.Error("invalid messages")
							return fmt.Errorf("invalid messages")
						}

						for _, message := range messages.GetMessages() {
							switch m := message.(type) {
							case *tg.Message:
								if m.Media != nil {
									switch media := m.Media.(type) {
									case *tg.MessageMediaDocument:
										document, ok := media.GetDocument()
										if !ok {
											continue
										}

										doc, ok := document.AsNotEmpty()
										if !ok {
											continue
										}

										select {
										case <-ctx.Done():
											r.log.Error("context canceled")
											return ctx.Err()

										case documents <- doc:
											r.log.Info("found document", zap.Int64("id", doc.GetID()), zap.String("mime_type", doc.MimeType), zap.Int64("size", doc.Size))
											hasher.Update64(uint64(doc.GetID()))
										}

									case *tg.MessageMediaPhoto:
										photo, ok := media.GetPhoto()
										if !ok {
											continue
										}

										p, ok := photo.AsNotEmpty()
										if !ok {
											continue
										}

										select {
										case <-ctx.Done():
											r.log.Error("context canceled")
											return ctx.Err()

										case photos <- p:
											r.log.Info("found photo", zap.Int64("id", p.GetID()))
											hasher.Update64(uint64(p.GetID()))
										}

									default:
										continue
									}
								}
							}
						}
					}
				})

				// check if output directory exists
				if _, err := os.Stat(output); os.IsNotExist(err) {
					err = os.MkdirAll(output, 0755)
					if err != nil {
						r.log.Error("failed to create output directory", zap.Error(err))
						return err
					}
				}

				pw := progress.NewWriter()
				pw.SetAutoStop(true)
				pw.SetTrackerLength(25)
				pw.SetMessageWidth(35)
				pw.SetNumTrackersExpected(5)
				pw.SetSortBy(progress.SortByPercentDsc)
				pw.SetStyle(progress.StyleDefault)
				pw.SetTrackerPosition(progress.PositionRight)
				pw.SetUpdateFrequency(time.Millisecond * 100)
				pw.Style().Colors = progress.StyleColorsExample
				pw.Style().Options.PercentFormat = "%4.1f%%"
				pw.Style().Visibility.ETA = true
				pw.Style().Visibility.ETAOverall = true

				go pw.Render()

				g.Go(func() error {
					d := downloader.NewDownloader()
					for {
						select {
						case <-ctx.Done():
							r.log.Error("context canceled")
							return ctx.Err()

						case doc := <-documents:
							ext, err := mime.ExtensionsByType(doc.MimeType)
							if err != nil {
								r.log.Error("failed to get extension", zap.Error(err))
								ext = []string{".bin"}
							}

							r.log.Info("downloading document", zap.Int64("id", doc.GetID()), zap.String("mime_type", doc.MimeType), zap.Int64("size", doc.Size))

							filename := fmt.Sprintf("%d%s", doc.GetID(), ext[0])
							docPath := filepath.Join(output, filename)
							if _, err := os.Stat(docPath); err == nil {
								continue
							}

							file, err := os.Create(filepath.Clean(docPath))
							if err != nil {
								r.log.Error("failed to create file", zap.Error(err))
								continue
							}

							tracker := &progress.Tracker{
								Message: fmt.Sprintf("Downloading %s", filename),
								Total:   doc.Size,
								Units:   progress.UnitsBytes,
							}

							pw.AppendTracker(tracker)

							builder := d.Download(c, doc.AsInputDocumentFileLocation())
							if _, err := builder.Stream(ctx, writerFunc(func(p []byte) (int, error) {
								select {
								case <-ctx.Done():
									tracker.MarkAsErrored()
									r.log.Error("context canceled")
									return 0, ctx.Err()

								default:
								}

								n, err := file.Write(p)
								if err != nil {
									tracker.MarkAsErrored()
									return n, err
								}

								tracker.Increment(int64(n))
								return n, nil
							})); err != nil {
								r.log.Error("failed to download document", zap.Error(err))
								continue
							}

							r.log.Info("downloaded document", zap.Int64("id", doc.GetID()))

						case photo := <-photos:
							fileID := fileid.FromPhoto(photo, 'x')

							filename := fmt.Sprintf("%d.jpg", photo.GetID())
							photoPath := filepath.Join(output, filename)
							if _, err := os.Stat(photoPath); err == nil {
								continue
							}

							loc, ok := fileID.AsInputFileLocation()
							if !ok {
								continue
							}

							r.log.Info("downloading photo", zap.Int64("id", photo.GetID()))
							if _, err := d.Download(c, loc).ToPath(ctx, photoPath); err != nil {
								r.log.Error("failed to download photo", zap.Error(err))
								continue
							}

							r.log.Info("downloaded photo", zap.Int64("id", photo.GetID()))
						}
					}
				})

				return g.Wait()
			})
		},
	}

	cmd.Flags().Int64P("id", "i", 0, "ID of the chat, channel or user")
	cmd.Flags().StringP("output", "o", "", "Output directory")
	cmd.Flags().IntP("limit", "l", 100, "Limit of messages to download")
	cmd.MarkFlagRequired("id")
	cmd.MarkFlagRequired("output")
	return cmd
}

/*func getExtensionByFileID(fileID fileid.FileID) string {
	switch fileID.Type {
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

func downloadFile(ctx context.Context, c *tg.Client, fileID fileid.FileID, output string, pw progress.Writer) error {
	filename := fmt.Sprintf("%d%s", fileID.ID, getExtensionByFileID(fileID))
	docPath := filepath.Join(output, filename)
	if _, err := os.Stat(docPath); err == nil {
		return fmt.Errorf("file already exists: %s", docPath)
	}

	file, err := os.Create(filepath.Clean(docPath))
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}

	tracker := &progress.Tracker{
		Message: fmt.Sprintf("Downloading %s", filename),
		Total:   doc.Size,
		Units:   progress.UnitsBytes,
	}

	pw.AppendTracker(tracker)

	loc, ok := fileID.AsInputFileLocation()
	if !ok {
		return fmt.Errorf("failed to get file location")
	}

	builder := d.Download(c, loc)
	if _, err := builder.Stream(ctx, writerFunc(func(p []byte) (int, error) {
		select {
		case <-ctx.Done():
			tracker.MarkAsErrored()
			return 0, ctx.Err()

		default:
		}

		n, err := file.Write(p)
		if err != nil {
			tracker.MarkAsErrored()
			return n, err
		}

		tracker.Increment(int64(n))
		return n, nil
	})); err != nil {
		return fmt.Errorf("failed to download document: %w", err)
	}

	return nil
}*/
