package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/jedib0t/go-pretty/v6/progress"
	"github.com/johnnyipcom/tgdownloader/pkg/telegram"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) {
	return f(p)
}

func createDirectoryIfNotExists(path string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		err = os.MkdirAll(path, 0755)
		if err != nil {
			return err
		}
	}

	return nil
}

func moveFile(src, dst string) error {
	err := os.Rename(src, dst)
	if err != nil {
		return err
	}

	return nil
}

func newDownloadCmd(ctx context.Context, r *Root) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "download",
		Short: "Download files from a chat, channel or user",
		Long:  `Download files from a chat, channel or user.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return r.client.Run(ctx, func(ctx context.Context, c telegram.Client) error {
				output, err := cmd.Flags().GetString("output")
				if err != nil {
					r.log.Error("failed to get output", zap.Error(err))
					return err
				}

				temp, err := cmd.Flags().GetString("temp")
				if err != nil {
					r.log.Error("failed to get temp", zap.Error(err))
					return err
				}

				limit, err := cmd.Flags().GetInt("limit")
				if err != nil {
					r.log.Error("failed to get limit", zap.Error(err))
					return err
				}

				user, err := cmd.Flags().GetInt64("user")
				if err != nil {
					r.log.Error("failed to get user", zap.Error(err))
					return err
				}

				chatID, err := strconv.ParseInt(args[0], 10, 64)
				if err != nil {
					r.log.Error("failed to convert chatID", zap.Error(err))
					return err
				}

				chat, err := c.FindChat(ctx, chatID)
				if err != nil {
					r.log.Error("failed to find chat", zap.Error(err))
					return err
				}

				r.log.Info("found chat", zap.String("title", chat.Title))

				err = createDirectoryIfNotExists(output)
				if err != nil {
					r.log.Error("failed to create directory", zap.Error(err))
					return err
				}

				err = createDirectoryIfNotExists(temp)
				if err != nil {
					r.log.Error("failed to create directory", zap.Error(err))
					return err
				}

				defer func() {
					if err := os.RemoveAll(temp); err != nil {
						r.log.Error("failed to remove temp directory", zap.Error(err))
					}

					r.log.Info("removed temp directory")
				}()

				pw := progress.NewWriter()
				pw.SetAutoStop(false)
				pw.SetTrackerLength(25)
				pw.SetMessageWidth(35)
				pw.SetSortBy(progress.SortByPercentDsc)
				pw.SetStyle(progress.StyleDefault)
				pw.SetTrackerPosition(progress.PositionRight)
				pw.SetUpdateFrequency(time.Millisecond * 100)
				pw.Style().Colors = progress.StyleColorsExample
				pw.Style().Options.PercentFormat = "%4.1f%%"
				pw.Style().Visibility.ETA = true
				pw.Style().Visibility.ETAOverall = true

				go pw.Render()

				defer pw.Stop()

				var getFileOptions []telegram.GetFileOption
				if limit != 0 {
					getFileOptions = append(getFileOptions, telegram.GetFileWithLimit(limit))
				}

				if user != 0 {
					getFileOptions = append(getFileOptions, telegram.GetFileWithUserID(user))
				}

				files, errors := c.GetFiles(ctx, chat, getFileOptions...)

				g, ctx := errgroup.WithContext(ctx)
				g.Go(func() error {
					for {
						select {
						case <-ctx.Done():
							r.log.Error("context canceled")
							return ctx.Err()

						case err := <-errors:
							r.log.Error("failed to get documents", zap.Error(err))

						case file, ok := <-files:
							if !ok {
								return nil
							}

							r.log.Info("found file", zap.Int64("fileID", file.ID()))

							filename := fmt.Sprintf("%d%s", file.ID(), file.GetExtension())
							tempPath := filepath.Join(temp, filename)
							if _, err := os.Stat(tempPath); err == nil {
								continue
							}

							f, err := os.Create(filepath.Clean(tempPath))
							if err != nil {
								r.log.Error("failed to create file", zap.Error(err))
								continue
							}

							tracker := &progress.Tracker{
								Message: fmt.Sprintf("Downloading %s", filename),
								Total:   file.Size(),
								Units:   progress.UnitsBytes,
							}

							pw.AppendTracker(tracker)
							if err := c.Download(ctx, file, writerFunc(func(p []byte) (int, error) {
								select {
								case <-ctx.Done():
									tracker.MarkAsErrored()
									return 0, ctx.Err()

								default:
								}

								n, err := f.Write(p)
								if err != nil {
									tracker.MarkAsErrored()
									return n, err
								}

								tracker.Increment(int64(n))
								return n, nil
							})); err != nil {
								tracker.MarkAsErrored()
								f.Close()

								if err := os.Remove(tempPath); err != nil {
									r.log.Error("failed to remove file", zap.Error(err))
								}

								r.log.Error("failed to download document", zap.Error(err))
								continue
							}

							tracker.MarkAsDone()
							f.Close()
							r.log.Info("downloaded document", zap.Int64("id", file.ID()))

							dstPath := filepath.Join(output, filename)
							if err := moveFile(tempPath, dstPath); err != nil {
								r.log.Error("failed to move file", zap.Error(err))
								continue
							}

							r.log.Info("moved file", zap.String("src", tempPath), zap.String("dst", dstPath))
						}
					}
				})

				return g.Wait()
			})
		},
	}

	cmd.Flags().StringP("output", "o", "./downloads", "Output directory")
	cmd.Flags().StringP("temp", "t", "./downloads/tmp", "Temporary directory")
	cmd.Flags().IntP("limit", "l", 0, "Limit of files to download")
	cmd.Flags().Int64P("user", "u", 0, "User ID to download from")
	return cmd
}
