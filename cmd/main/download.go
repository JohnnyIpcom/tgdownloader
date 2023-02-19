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
	type downloadOptions struct {
		output     string
		temp       string
		limit      int
		user       int64
		offsetDate string
	}

	var opts downloadOptions

	cmd := &cobra.Command{
		Use:   "download",
		Short: "Download files from a chat, channel or user",
		Long:  `Download files from a chat, channel or user.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return r.client.Run(ctx, func(ctx context.Context, c telegram.Client) error {
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

				err = createDirectoryIfNotExists(opts.output)
				if err != nil {
					r.log.Error("failed to create directory", zap.Error(err))
					return err
				}

				err = createDirectoryIfNotExists(opts.temp)
				if err != nil {
					r.log.Error("failed to create directory", zap.Error(err))
					return err
				}

				defer func() {
					if err := os.RemoveAll(opts.temp); err != nil {
						r.log.Error("failed to remove temp directory", zap.Error(err))
					}

					r.log.Info("removed temp directory")
				}()

				pw := progress.NewWriter()
				pw.SetAutoStop(false)
				pw.SetTrackerLength(25)
				pw.SetMessageWidth(45)
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
				getFileOptions = append(getFileOptions, telegram.GetFileWithUserID(opts.user))

				if opts.limit > 0 {
					getFileOptions = append(getFileOptions, telegram.GetFileWithLimit(opts.limit))
				}

				if opts.offsetDate != "" {
					offsetDate, err := time.Parse("2006-01-02 15:04:05", opts.offsetDate)
					if err != nil {
						r.log.Error("failed to parse offset date", zap.Error(err))
						return err
					}

					getFileOptions = append(getFileOptions, telegram.GetFileWithOffsetDate(int(offsetDate.Unix())))
				}

				files, errors := c.GetFiles(ctx, chat, getFileOptions...)

				g, ctx := errgroup.WithContext(ctx)
				for i := 0; i < 5; i++ {
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

								r.log.Info("found file", zap.Stringer("file", file))

								tempPath := filepath.Join(opts.temp, file.Filename())
								if _, err := os.Stat(tempPath); err == nil {
									continue
								}

								f, err := os.Create(filepath.Clean(tempPath))
								if err != nil {
									r.log.Error("failed to create file", zap.Error(err))
									continue
								}

								tracker := &progress.Tracker{
									Message: file.Filename(),
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
								r.log.Info("downloaded document", zap.String("filename", file.Filename()))

								userFolder := fmt.Sprintf("%d", file.FromID())
								output := filepath.Join(opts.output, userFolder)

								err = createDirectoryIfNotExists(output)
								if err != nil {
									r.log.Error("failed to create directory", zap.Error(err))
									continue
								}

								dstPath := filepath.Join(output, file.Filename())
								if err := moveFile(tempPath, dstPath); err != nil {
									r.log.Error("failed to move file", zap.Error(err))
									continue
								}

								r.log.Info("moved file", zap.String("src", tempPath), zap.String("dst", dstPath))
							}
						}
					})
				}

				return g.Wait()
			})
		},
	}

	cmd.Flags().StringVarP(&opts.output, "output", "o", "./downloads", "Output directory")
	cmd.Flags().StringVarP(&opts.temp, "temp", "t", "./downloads/tmp", "Temporary directory")
	cmd.Flags().IntVarP(&opts.limit, "limit", "l", 0, "Limit of files to download")
	cmd.Flags().Int64VarP(&opts.user, "user", "u", 0, "User ID to download from")
	cmd.Flags().StringVarP(&opts.offsetDate, "offset-date", "d", "", "Offset date to download from, format: 2006-01-02 15:04:05")
	return cmd
}
