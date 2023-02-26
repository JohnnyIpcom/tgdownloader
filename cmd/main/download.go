package cmd

import (
	"context"
	"strconv"
	"time"

	"github.com/jedib0t/go-pretty/v6/progress"
	"github.com/johnnyipcom/tgdownloader/pkg/downloader"
	"github.com/johnnyipcom/tgdownloader/pkg/telegram"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) {
	return f(p)
}

func newProgressWriter() progress.Writer {
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
	return pw
}

type fileWrapper struct {
	f telegram.FileInfo
}

var _ downloader.FileInfo = (*fileWrapper)(nil)

func (f *fileWrapper) Filename() string {
	return f.f.Filename()
}

func (f *fileWrapper) Subdir() string {
	if f.f.Username() != "" {
		return f.f.Username()
	}

	return strconv.FormatInt(f.f.FromID(), 10)
}

func newDownloadCmd(ctx context.Context, r *Root) *cobra.Command {
	type downloadOptions struct {
		limit      int
		user       int64
		offsetDate string
		observer   bool
	}

	var opts downloadOptions

	cmdDownload := &cobra.Command{
		Use:   "download",
		Short: "Download files from a chat, channel or user",
		Long:  `Download files from a chat, channel or user.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ID, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				r.log.Error("failed to convert chatID", zap.Error(err))
				return err
			}

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

			var runOptions []telegram.RunOption
			if opts.observer {
				runOptions = append(runOptions, telegram.RunInfinite())
			}

			if err := r.downloader.Prepare(); err != nil {
				r.log.Error("failed to prepare downloader", zap.Error(err))
				return err
			}

			return r.client.Run(ctx, func(ctx context.Context, c telegram.Client) error {
				defer r.downloader.Cleanup()

				pw := newProgressWriter()
				defer pw.Stop()

				var files <-chan telegram.FileInfo
				var errors <-chan error

				if opts.observer {
					files, errors = c.GetFilesFromNewMessages(ctx, ID)
				} else {
					files, errors = c.GetFiles(ctx, ID, getFileOptions...)
				}

				g, ctx := errgroup.WithContext(ctx)
				for i := 0; i < 5; i++ {
					g.Go(func() error {
						for {
							select {
							case <-ctx.Done():
								return ctx.Err()

							case err := <-errors:
								r.log.Error("failed to get file", zap.Error(err))

							case file, ok := <-files:
								if !ok {
									r.log.Info("no more files")
									return nil
								}

								r.log.Debug("found file", zap.Stringer("file", file))

								f, err := r.downloader.Create(ctx, &fileWrapper{
									f: file,
								})
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

									r.log.Error("failed to download document", zap.Error(err))
									if err := f.Abort(); err != nil {
										r.log.Error("failed to abort file", zap.Error(err))
									}

									continue
								}

								if err := f.Commit(); err != nil {
									r.log.Error("failed to commit file", zap.Error(err))
									tracker.MarkAsErrored()
									continue
								}

								tracker.MarkAsDone()
								r.log.Debug("downloaded document", zap.String("filename", file.Filename()))
							}
						}
					})
				}

				return g.Wait()
			}, runOptions...)
		},
	}

	cmdDownload.Flags().BoolVarP(&opts.observer, "observer", "O", false, "Enable observer mode")
	cmdDownload.Flags().IntVarP(&opts.limit, "limit", "l", 0, "Limit of files to download")
	cmdDownload.Flags().Int64VarP(&opts.user, "user", "u", 0, "User ID to download from")
	cmdDownload.Flags().StringVarP(&opts.offsetDate, "offset-date", "d", "", "Offset date to download from, format: 2006-01-02 15:04:05")
	return cmdDownload
}
